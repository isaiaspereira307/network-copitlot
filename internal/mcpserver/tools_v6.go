package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/scanner"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

func timeNowUnixMilli() int64 { return time.Now().UnixMilli() }

func registerV6Tools(s *Server) {
	s.tools["scan_passive_run"] = s.toolScanPassiveRun
	s.tools["scan_passive_status"] = s.toolScanPassiveStatus
	s.tools["list_findings"] = s.toolListFindings
	s.tools["get_finding_detail"] = s.toolGetFindingDetail
	s.tools["finding_set_status"] = s.toolFindingSetStatus
	s.tools["get_sitemap"] = s.toolGetSitemap
}

// scanJob rastreia o andamento de um scan passivo (assincrono).
type scanJob struct {
	mu      sync.Mutex
	id      string
	status  string // queued|running|done|error
	scanned int
	counts  map[string]int
	err     string
	done    bool
}

func (s *Server) ensureFindingsSchema(st store.Store) error { return st.EnsureFindingsSchema() }

// toolScanPassiveRun roda o scanner passivo sobre todos os requests capturados
// do alvo ativo (NÃO envia nada ao host: so le o que ja foi capturado). Persiste
// os achados como findings e retorna um job id para scan_passive_status.
func (s *Server) toolScanPassiveRun(ctx context.Context, args map[string]any) (string, error) {
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

	jobID := fmt.Sprintf("passive-%d", timeNowUnixMilli())
	s.scanMu.Lock()
	j := &scanJob{id: jobID, status: "queued", counts: map[string]int{}}
	s.scans[jobID] = j
	s.scanMu.Unlock()

	reqs, err := st.All()
	if err != nil {
		return "", err
	}
	scopeHost := tgt.Host

	go func() {
		j.mu.Lock()
		j.status = "running"
		j.mu.Unlock()
		result := scanner.RunPassive(reqs, scopeHost)
		for _, dets := range result.ByType {
			for _, d := range dets {
				_, aerr := st.AddFinding(&store.Finding{
					Type:      d.Type,
					Severity:  d.Severity,
					URL:       d.Evidence["url"].(string),
					RequestID: idFromEv(d.Evidence),
					Evidence:  store.EvidenceJSON(d.Evidence),
				})
				if aerr != nil {
					continue // nao aborta o scan por um finding falho
				}
			}
		}
		j.mu.Lock()
		j.status = "done"
		j.scanned = result.Total
		j.counts = result.Hits
		j.done = true
		j.mu.Unlock()
		s.audit.Log(audit.Event{Tool: "scan_passive_run", Action: "scan",
			Detail: map[string]any{"job": jobID, "host": tgt.Host, "scanned": result.Total, "hits": result.Hits}})
	}()

	out := map[string]any{"job_id": jobID, "status": "queued"}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (s *Server) toolScanPassiveStatus(ctx context.Context, args map[string]any) (string, error) {
	jobID := argStr(args, "job_id")
	s.scanMu.Lock()
	j := s.scans[jobID]
	s.scanMu.Unlock()
	if j == nil {
		return "", fmt.Errorf("scan job %s nao encontrado", jobID)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]any{"job_id": j.id, "status": j.status, "scanned": j.scanned, "counts": j.counts}
	if j.err != "" {
		out["error"] = j.err
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (s *Server) toolListFindings(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo")
	}
	typeFilter := argStr(args, "type")
	severity := argStr(args, "severity")
	list, err := st.ListFindings(typeFilter, severity)
	if err != nil {
		return "", err
	}
	out := map[string]any{"count": len(list), "findings": list}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (s *Server) toolGetFindingDetail(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo")
	}
	id, ok := argInt(args, "finding_id")
	if !ok {
		return "", fmt.Errorf("finding_id obrigatorio")
	}
	f, err := st.GetFinding(id)
	if err != nil {
		return "", err
	}
	out := map[string]any{
		"id": f.ID, "type": f.Type, "severity": f.Severity, "url": f.URL,
		"request_id": f.RequestID, "status": f.Status, "notes": f.Notes,
		"evidence": store.ParseEvidence(f.Evidence),
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (s *Server) toolFindingSetStatus(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo")
	}
	id, ok := argInt(args, "finding_id")
	if !ok {
		return "", fmt.Errorf("finding_id obrigatorio")
	}
	status := argStr(args, "status")
	if !store.FindingStatusValid(status) {
		return "", fmt.Errorf("status invalido: %q (use open|triaged|confirmed|false-positive|closed)", status)
	}
	if err := st.SetFindingStatus(id, store.FindingStatus(status)); err != nil {
		return "", err
	}
	return fmt.Sprintf("finding %d -> %s", id, status), nil
}

func (s *Server) toolGetSitemap(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo")
	}
	reqs, err := st.All()
	if err != nil {
		return "", err
	}
	nodes := scanner.BuildSitemap(reqs)
	out := map[string]any{"count": len(nodes), "sitemap": nodes}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// idFromEv extrai o request_id do mapa de evidencias (0 se ausente).
func idFromEv(m map[string]any) int64 {
	if v, ok := m["request_id"].(float64); ok {
		return int64(v)
	}
	if v, ok := m["request_id"].(int64); ok {
		return v
	}
	return 0
}
