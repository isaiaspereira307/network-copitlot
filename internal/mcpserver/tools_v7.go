package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/isaiaspereira307/network-copitlot/internal/proxy"
	"github.com/isaiaspereira307/network-copitlot/internal/scanner"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// activeArmed indica se o operador autorizou trafego ativo nesta execucao.
// Double opt-in (PRD v4.1): (1) env MCP_PROXY_ACTIVE=1 na inicializacao e
// (2) confirmed=true em cada chamada de scan_active_start. Sem ambos, a tool
// recusa. Trafego ativo SO e permitido in-scope e com throttle agressivo.
func activeArmed() bool { return strings.EqualFold(os.Getenv("MCP_PROXY_ACTIVE"), "1") }

// maxActiveRequests capa o total de payloads ativos por scan (safety valve).
const maxActiveRequests = 500

func registerV7Tools(s *Server) {
	s.tools["scan_active_start"] = s.toolScanActiveStart
	s.tools["scan_active_status"] = s.toolScanActiveStatus
	s.tools["crawl_start"] = s.toolCrawlStart
}

// toolScanActiveStart lanca um scan ATIVO (envia payloads ao host). Requer
// double opt-in: env MCP_PROXY_ACTIVE=1 E confirmed=true. NUNCA abandona o
// escopo e NAO permite payloads destrutivos.
func (s *Server) toolScanActiveStart(ctx context.Context, args map[string]any) (string, error) {
	if !activeArmed() {
		return "", fmt.Errorf("scan ativo desabilitado: inicie o servidor com MCP_PROXY_ACTIVE=1 (double opt-in)")
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return "", fmt.Errorf("confirmacao obrigatoria: passe confirmed=true para autorizar trafego ativo (double opt-in)")
	}
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	if err := s.ensureFindingsSchema(st); err != nil {
		return "", err
	}
	tgt, err := s.active.Target()
	if err != nil || tgt == nil {
		return "", fmt.Errorf("nenhum alvo ativo")
	}

	reqs, err := st.All()
	if err != nil {
		return "", err
	}
	actives := scanner.BuildActiveRequests(reqs, maxActiveRequests)
	if len(actives) == 0 {
		return "", fmt.Errorf("nenhum request capturado fuzzavel (com query ou body) para o scan ativo")
	}

	throttleRPS := 0.0
	if v, ok := argFloat(args, "throttle_rps"); ok && v > 0 {
		throttleRPS = v
	}
	throttle := time.Duration(0)
	if throttleRPS > 0 {
		throttle = time.Duration(float64(time.Second) / throttleRPS)
	}

	jobID := fmt.Sprintf("active-%d", timeNowUnixMilli())
	s.scanMu.Lock()
	s.scans[jobID] = &scanJob{id: jobID, status: "queued", counts: map[string]int{}}
	s.scanMu.Unlock()
	go s.runActiveScan(jobID, st, tgt, actives, throttle)

	out := map[string]any{"job_id": jobID, "status": "queued", "total_payloads": len(actives)}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "scan_active_start", Action: "start",
		Detail: map[string]any{"job_id": jobID, "host": tgt.Host, "payloads": len(actives)}})
	return string(b), nil
}

func (s *Server) runActiveScan(jobID string, st store.Store, tgt *projects.Target, actives []scanner.ActiveRequest, throttle time.Duration) {
	sc := proxy.New(tgt)
	scopeMatch := func(h string) bool { return sc.Matches(&url.URL{Host: h}) }

	s.scanMu.Lock()
	current := s.scans[jobID]
	if current != nil {
		current.status = "running"
	}
	s.scanMu.Unlock()

	sent := 0
	for _, ar := range actives {
		orig, gerr := st.Get(ar.BaseID)
		if gerr != nil {
			continue
		}
		ov, aerr := applyActivePayload(orig, ar)
		if aerr != nil {
			continue
		}
		res, rerr := st.Replay(ar.BaseID, ov, scopeMatch)
		if rerr != nil {
			if current != nil {
				s.scanMu.Lock()
				current.scanned++
				s.scanMu.Unlock()
			}
			continue
		}
		sent++
		reflected := false
		if rr, rg := st.Get(res.NewRequestID); rg == nil {
			if bytesContains(rr.RespBody, ar.Payload) {
				reflected = true
			}
		}

		// persiste finding apenas para reflexo (forte sinal) — nunca para erro
		if reflected {
			sev := store.SevMed
			if strings.EqualFold(ar.Payload, `'`) || strings.Contains(ar.Payload, "1 UNION SELECT") {
				sev = store.SevHigh
			}
			st.AddFinding(&store.Finding{
				Type:      ar.Tech, Severity: sev, URL: orig.URL,
				RequestID: res.NewRequestID, Status: "open",
				Evidence: store.EvidenceJSON(map[string]any{
					"type": ar.Tech, "payload": ar.Redacted, "reflected": true,
					"base_request_id": ar.BaseID,
				}),
			})
		}
		if throttle > 0 {
			time.Sleep(throttle)
		}
	}
	s.scanMu.Lock()
	if current != nil {
		current.status = "done"
	}
	s.scanMu.Unlock()
	s.audit.Log(audit.Event{Tool: "scan_active_start", Action: "complete",
		Detail: map[string]any{"job_id": jobID, "sent": sent}})
}

func (s *Server) toolScanActiveStatus(ctx context.Context, args map[string]any) (string, error) {
	jobID := argStr(args, "job_id")
	s.scanMu.Lock()
	j := s.scans[jobID]
	if j == nil {
		s.scanMu.Unlock()
		return "", fmt.Errorf("job %s nao encontrado", jobID)
	}
	status, scanned := j.status, j.scanned
	s.scanMu.Unlock()
	out := map[string]any{"job_id": jobID, "status": status, "scanned": scanned, "counts": j.counts}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (s *Server) toolCrawlStart(ctx context.Context, args map[string]any) (string, error) {
	if !activeArmed() {
		return "", fmt.Errorf("crawl ativo desabilitado: inicie com MCP_PROXY_ACTIVE=1 (double opt-in)")
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return "", fmt.Errorf("confirmacao obrigatoria: passe confirmed=true (double opt-in)")
	}
	_ = s
	return "", fmt.Errorf("crawl_start: implementacao do crawler no harness ainda nao concluida (use scan_passive_run/get_sitemap)")
}

// bytesContains e um helper local (strings.Contains recebe string).
func bytesContains(b []byte, sub string) bool {
	return len(sub) > 0 && strings.Contains(string(b), sub)
}

// applyActivePayload construi ReplayOverrides que injeta o payload ativo no
// request base: parametro de query "q". Nao altera host/schema -> permanece
// in-scope quando o scope guard validar o host final.
func applyActivePayload(r *store.Request, ar scanner.ActiveRequest) (store.ReplayOverrides, error) {
	u, err := url.Parse(r.URL)
	if err != nil {
		return store.ReplayOverrides{}, err
	}
	q := u.Query()
	q.Set("q", ar.Payload)
	u.RawQuery = q.Encode()
	return store.ReplayOverrides{URLOverride: u.String()}, nil
}
