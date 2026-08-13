package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/summarize"
)

func registerV3Tools(s *Server) {
	// endpoints (task 14)
	s.tools["list_endpoints"] = s.toolListEndpoints
	// diff (task 15)
	s.tools["diff_requests"] = s.toolDiffRequests
	// summarize (task 16)
	s.tools["summarize_response"] = s.toolSummarizeResponse
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

// toolSummarizeResponse resume o body da response de um request capturado do
// alvo ativo por content-type: HTML (forms/links/scripts/comments), JSON
// (chaves+tipos, nunca valores), JS (endpoints/calls/tokens). Corpos grandes
// sao analisados de um prefixo limitado (truncated=true, total_len=N). Nunca
// despeja o body; para o corpo completo use get_request_detail.
func (s *Server) toolSummarizeResponse(ctx context.Context, args map[string]any) (string, error) {
	id, ok := argInt(args, "id")
	if !ok {
		return "", fmt.Errorf("id e obrigatorio (numero)")
	}
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "summarize_response", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	r, err := st.Get(id)
	if err != nil {
		return "", fmt.Errorf("request %d nao encontrado", id)
	}
	ct := ""
	if v := r.RespHeaders["Content-Type"]; len(v) > 0 {
		ct = v[0]
	}
	res := summarize.Body(ct, r.RespBody, summarize.MaxBodyBytes)
	out := map[string]any{
		"id":           id,
		"url":          r.URL,
		"status":       r.Status,
		"content_type": ct,
		"kind":         res.Kind,
		"truncated":    res.Truncated,
		"total_len":    res.TotalLen,
	}
	if res.Detail != nil {
		out["detail"] = res.Detail
	}
	if res.Note != "" {
		out["note"] = res.Note
	}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "summarize_response", Action: "summarize",
		Detail: map[string]any{"id": id, "kind": res.Kind, "total_len": res.TotalLen, "truncated": res.Truncated}})
	return string(b), nil
}
