package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/isaias/network-copitlot/internal/audit"
)

func registerV3Tools(s *Server) {
	// endpoints (task 14)
	s.tools["list_endpoints"] = s.toolListEndpoints
	// diff (task 15)
	s.tools["diff_requests"] = s.toolDiffRequests
}

// maxDiffOutLines/maxDiffOutBytes limitam o diff emitido: frugalidade de
// tokens. Acima disso o output sinaliza truncated + total para paginar/refinar.
const (
	maxDiffOutLines = 200
	maxDiffOutBytes = 8 * 1024
)

// toolDiffRequests diffs dois requests capturados do alvo ativo (ids de
// list_requests). mode escolhe o que comparar: resp (default, response bodies),
// req (request bodies) ou headers (cabecalhos req+resp como linhas). Devolve
// diff unificado minimal por linha (prefixo ' '/'-'/'+') com resumos
// changed_a/changed_b; o texto e truncado (truncated=true, total=...) para
// nunca estourar a janela de contexto. Nunca expoe bodies fora das linhas do diff.
func (s *Server) toolDiffRequests(ctx context.Context, args map[string]any) (string, error) {
	idA, okA := argInt(args, "id_a")
	idB, okB := argInt(args, "id_b")
	if !okA || !okB {
		return "", fmt.Errorf("id_a e id_b sao obrigatorios (numero)")
	}
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "diff_requests", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	d, err := st.DiffRequests(idA, idB, argStr(args, "mode"))
	if err != nil {
		s.audit.Log(audit.Event{Tool: "diff_requests", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	ra, _ := st.Get(idA)
	rb, _ := st.Get(idB)

	diff, total, truncated := capDiff(d.Lines, maxDiffOutLines, maxDiffOutBytes)
	out := map[string]any{
		"id_a":       idA,
		"id_b":       idB,
		"mode":       d.Mode,
		"changed_a":  len(d.ChangedAB),
		"changed_b":  len(d.ChangedBA),
		"diff":       diff,
		"diff_lines": len(diff),
		"total":      total,
		"truncated":  truncated,
	}
	if ra != nil {
		out["a"] = map[string]any{"url": ra.URL, "status": ra.Status, "resp_len": ra.RespLen}
	}
	if rb != nil {
		out["b"] = map[string]any{"url": rb.URL, "status": rb.Status, "resp_len": rb.RespLen}
	}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "diff_requests", Action: "diff",
		Detail: map[string]any{"id_a": idA, "id_b": idB, "mode": d.Mode, "changed": len(d.ChangedAB) + len(d.ChangedBA), "truncated": truncated}})
	return string(b), nil
}

// capDiff trunca lines ao orcamento (linhas E bytes) sem partir linha no meio;
// devolve o total original p/ sinalizar truncation.
func capDiff(lines []string, maxLines, maxBytes int) ([]string, int, bool) {
	out := make([]string, 0, len(lines))
	total := 0
	for _, l := range lines {
		if len(out) >= maxLines || total+len(l) > maxBytes {
			return out, len(lines), true
		}
		out = append(out, l)
		total += len(l)
	}
	return out, len(lines), false
}

// toolListEndpoints lista os endpoints deduplicados do alvo ativo: agrega por
// (method, path normalizado) com segmentos dinamicos ({id}) colapsados, e
// reporta hit_count total + ate 5 sample_ids (mais recentes) para follow-up.
// Nunca expoe corpos.
func (s *Server) toolListEndpoints(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "list_endpoints", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	eps, err := st.ListEndpoints()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "list_endpoints", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	// ponytail: map manual p/ keys minusculas; Endpoint sem json tags.
	epsOut := make([]map[string]any, 0, len(eps))
	for _, e := range eps {
		epsOut = append(epsOut, map[string]any{
			"method":     e.Method,
			"path":       e.Path,
			"hit_count":  e.HitCount,
			"sample_ids": e.SampleIDs,
		})
	}
	out := map[string]any{"count": len(epsOut), "endpoints": epsOut}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "list_endpoints", Action: "list", Detail: map[string]any{"count": len(epsOut)}})
	return string(b), nil
}
