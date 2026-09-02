package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/isaiaspereira307/network-copitlot/internal/proxy"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// maxFuzzPayloads limita quantos payloads um fuzz_request executa: cada payload
// e um replay real (rede + insert), entao capamos p/ nao virar DoS acidental
// no alvo nem estourar tempo/tokens. Acima disso: trunca e sinaliza.
const maxFuzzPayloads = 100

// maxFuzzRows limita as linhas devolvidas ao LLM (frugalidade de tokens); o
// fuzz roda TODOS os payloads mas so retorna as linhas mais interessantes
// (anomalias primeiro) ate esse teto, com truncated+total.
const maxFuzzRows = 50

// builtinPayloadSets sao conjuntos pequenos e representativos p/ triagem rapida
// (nao substituem uma wordlist dedicada). O conteudo do payload e DADO enviado
// ao host in-scope — nunca muda o host de destino, entao o scope guard do
// replay continua valendo.
var builtinPayloadSets = map[string][]string{
	"xss": {
		`<script>alert(1)</script>`,
		`"><img src=x onerror=alert(1)>`,
		`'><svg onload=alert(1)>`,
		`javascript:alert(1)`,
	},
	"sqli": {
		`'`,
		`' OR '1'='1`,
		`1' OR '1'='1'-- -`,
		`" OR ""="`,
		`1);SELECT pg_sleep(5)-- -`,
	},
	"traversal": {
		`../../../../etc/passwd`,
		`..%2f..%2f..%2f..%2fetc%2fpasswd`,
		`....//....//....//etc/passwd`,
		`..\..\..\..\windows\win.ini`,
	},
	"redirect": {
		`//evil.example`,
		`https://evil.example`,
		`/\evil.example`,
		`https:evil.example`,
	},
}

func registerV4Tools(s *Server) {
	// fuzz (intruder-lite): injeta payloads num ponto de um request capturado,
	// replaya cada um sob o scope guard e compara com o baseline.
	s.tools["fuzz_request"] = s.toolFuzzRequest
	// match/replace vivo no proxy (persistido no meta.yaml do alvo).
	s.tools["set_match_replace"] = s.toolSetMatchReplace
	s.tools["list_match_replace"] = s.toolListMatchReplace
}

// toolSetMatchReplace persiste a lista de regras de match/replace do alvo ativo
// (substitui a lista inteira; [] limpa). O proxy vivo aplica as regras
// habilitadas a cada request in-scope, recarregando via mtime do meta.yaml —
// sem restart. Cada regra e validada (part valido, regex compila).
func (s *Server) toolSetMatchReplace(ctx context.Context, args map[string]any) (string, error) {
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return "", fmt.Errorf("nenhum projeto ativo")
	}
	tgt, err := s.active.Target()
	if err != nil || tgt == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	raw, ok := args["rules"]
	if !ok {
		return "", fmt.Errorf("rules obrigatorio: array de {part, match, replace, header?, name?, enabled?} ([] limpa)")
	}
	list, _ := raw.([]any)
	rules := make([]projects.MatchReplaceRule, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return "", fmt.Errorf("rules[%d] invalido: esperado objeto", i)
		}
		enabled := true
		if v, ok := m["enabled"].(bool); ok {
			enabled = v
		}
		rules = append(rules, projects.MatchReplaceRule{
			Name:    argStr(m, "name"),
			Part:    argStr(m, "part"),
			Match:   argStr(m, "match"),
			Replace: argStr(m, "replace"),
			Header:  argStr(m, "header"),
			Enabled: enabled,
		})
	}
	if err := s.repo.SetMatchReplace(proj.Name, tgt.Host, rules); err != nil {
		s.audit.Log(audit.Event{Tool: "set_match_replace", Action: "error", Detail: map[string]any{"host": tgt.Host, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "set_match_replace", Action: "set", Detail: map[string]any{"host": tgt.Host, "count": len(rules)}})
	out := map[string]any{"host": tgt.Host, "rules_count": len(rules)}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// toolListMatchReplace devolve as regras de match/replace persistidas do alvo
// ativo (recarregadas do disco).
func (s *Server) toolListMatchReplace(ctx context.Context, args map[string]any) (string, error) {
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return "", fmt.Errorf("nenhum projeto ativo")
	}
	tgt, err := s.active.Target()
	if err != nil || tgt == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	fresh, err := s.repo.LoadTarget(proj.Name, tgt.Host)
	if err != nil {
		return "", err
	}
	out := map[string]any{"host": tgt.Host, "rules": fresh.MatchReplaceRules}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "list_match_replace", Action: "list", Detail: map[string]any{"host": tgt.Host, "count": len(fresh.MatchReplaceRules)}})
	return string(b), nil
}

type fuzzRow struct {
	Payload   string `json:"payload"`
	Status    int    `json:"status"`
	RespLen   int    `json:"resp_len"`
	Reflected bool   `json:"reflected"`
	NewID     int64  `json:"new_id"`
	Anomaly   bool   `json:"anomaly"`
	Err       string `json:"err,omitempty"`
}

// toolFuzzRequest re-executa um request capturado (id) uma vez por payload,
// injetando cada payload no `point` escolhido, sempre validado pelo scope guard
// do alvo ativo (cada replay reusa store.Replay). Compara status/resp_len com o
// baseline (request original) e marca anomalias (status mudou, tamanho variou
// >20%, ou o payload refletiu no corpo da resposta). Devolve tabela compacta —
// nunca corpos — com anomalias primeiro, truncada p/ frugalidade de tokens.
//
// point aceita:
//   - "marker"        -> substitui todas ocorrencias de `marker` (default FUZZ)
//     na URL, no body e nos valores de header
//   - "body"          -> substitui o body inteiro pelo payload
//   - "url"           -> substitui a URL inteira pelo payload
//   - "query:<param>" -> seta o parametro de query <param>=payload
//   - "header:<nome>" -> seta o header <nome>=payload
//
// payloads (array) e/ou payload_set (xss|sqli|traversal|redirect) fornecem os
// valores; ambos podem ser combinados. Cap de maxFuzzPayloads.
func (s *Server) toolFuzzRequest(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "fuzz_request", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	id, ok := argInt(args, "id")
	if !ok || id <= 0 {
		return "", fmt.Errorf("id obrigatorio: passe o id do request base (ex: 42)")
	}
	point := argStr(args, "point")
	if point == "" {
		return "", fmt.Errorf("point obrigatorio: marker | body | url | query:<param> | header:<nome>")
	}
	marker := argStr(args, "marker")
	if marker == "" {
		marker = "FUZZ"
	}

	payloads := collectPayloads(args)
	if len(payloads) == 0 {
		return "", fmt.Errorf("nenhum payload: passe payloads=[...] e/ou payload_set (xss|sqli|traversal|redirect)")
	}
	truncatedPayloads := false
	if len(payloads) > maxFuzzPayloads {
		payloads = payloads[:maxFuzzPayloads]
		truncatedPayloads = true
	}

	orig, err := st.Get(id)
	if err != nil {
		s.audit.Log(audit.Event{Tool: "fuzz_request", Action: "error", Detail: map[string]any{"id": id, "err": err.Error()}})
		return "", fmt.Errorf("request base %d nao encontrado: %w", id, err)
	}
	if point == "marker" && !markerPresent(orig, marker) {
		return "", fmt.Errorf("marker %q nao encontrado na URL/body/headers do request %d — insira o marker ou use point=body|url|query:<p>|header:<n>", marker, id)
	}

	tgt, err := s.active.Target()
	if err != nil || tgt == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	sc := proxy.New(tgt)
	scopeMatch := func(host string) bool { return sc.Matches(&url.URL{Host: host}) }
	followRedirects, _ := args["follow_redirects"].(bool)

	rows := make([]fuzzRow, 0, len(payloads))
	anomalies := 0
	for _, pl := range payloads {
		ov, buildErr := buildFuzzOverrides(orig, point, marker, pl)
		if buildErr != nil {
			rows = append(rows, fuzzRow{Payload: pl, Err: buildErr.Error()})
			continue
		}
		ov.FollowRedirects = followRedirects
		res, rErr := st.Replay(id, ov, scopeMatch)
		if rErr != nil {
			rows = append(rows, fuzzRow{Payload: pl, Err: rErr.Error()})
			continue
		}
		reflected := false
		if nr, gErr := st.Get(res.NewRequestID); gErr == nil && len(nr.RespBody) > 0 {
			reflected = strings.Contains(string(nr.RespBody), pl)
		}
		anom := isFuzzAnomaly(orig, res.Status, res.RespLen, reflected)
		if anom {
			anomalies++
		}
		rows = append(rows, fuzzRow{
			Payload: pl, Status: res.Status, RespLen: res.RespLen,
			Reflected: reflected, NewID: res.NewRequestID, Anomaly: anom,
		})
	}

	// Anomalias primeiro (estaveis por ordem original dentro de cada grupo).
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Anomaly && !rows[j].Anomaly
	})
	total := len(rows)
	truncatedRows := false
	if len(rows) > maxFuzzRows {
		rows = rows[:maxFuzzRows]
		truncatedRows = true
	}

	out := map[string]any{
		"id":        id,
		"point":     point,
		"count":     total,
		"anomalies": anomalies,
		"baseline":  map[string]any{"status": orig.Status, "resp_len": orig.RespLen},
		"results":   rows,
		"truncated": truncatedPayloads || truncatedRows,
	}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "fuzz_request", Action: "fuzz", Detail: map[string]any{
		"id": id, "point": point, "count": total, "anomalies": anomalies,
	}})
	return string(b), nil
}

// collectPayloads junta payloads explicitos (array) + payload_set builtin.
func collectPayloads(args map[string]any) []string {
	var out []string
	switch v := args["payloads"].(type) {
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, v...)
	}
	if set := argStr(args, "payload_set"); set != "" {
		out = append(out, builtinPayloadSets[strings.ToLower(set)]...)
	}
	return out
}

// markerPresent reporta se o marker aparece na URL, body ou algum valor de
// header do request base.
func markerPresent(r *store.Request, marker string) bool {
	if strings.Contains(r.URL, marker) || strings.Contains(string(r.ReqBody), marker) {
		return true
	}
	for _, vs := range r.ReqHeaders {
		for _, v := range vs {
			if strings.Contains(v, marker) {
				return true
			}
		}
	}
	return false
}

// buildFuzzOverrides monta os ReplayOverrides p/ um payload conforme o point.
func buildFuzzOverrides(orig *store.Request, point, marker, payload string) (store.ReplayOverrides, error) {
	var ov store.ReplayOverrides
	switch {
	case point == "marker":
		if strings.Contains(orig.URL, marker) {
			ov.URLOverride = strings.ReplaceAll(orig.URL, marker, payload)
		}
		if strings.Contains(string(orig.ReqBody), marker) {
			ov.BodyOverride = []byte(strings.ReplaceAll(string(orig.ReqBody), marker, payload))
		}
		for k, vs := range orig.ReqHeaders {
			for _, v := range vs {
				if strings.Contains(v, marker) {
					if ov.HeaderOverrides == nil {
						ov.HeaderOverrides = map[string]string{}
					}
					ov.HeaderOverrides[k] = strings.ReplaceAll(v, marker, payload)
				}
			}
		}
	case point == "body":
		ov.BodyOverride = []byte(payload)
	case point == "url":
		ov.URLOverride = payload
	case strings.HasPrefix(point, "query:"):
		param := strings.TrimPrefix(point, "query:")
		if param == "" {
			return ov, fmt.Errorf("point query: exige nome do parametro (ex: query:q)")
		}
		u, err := url.Parse(orig.URL)
		if err != nil {
			return ov, fmt.Errorf("url base invalida: %w", err)
		}
		q := u.Query()
		q.Set(param, payload)
		u.RawQuery = q.Encode()
		ov.URLOverride = u.String()
	case strings.HasPrefix(point, "header:"):
		name := strings.TrimPrefix(point, "header:")
		if name == "" {
			return ov, fmt.Errorf("point header: exige nome do header (ex: header:X-Forwarded-For)")
		}
		ov.HeaderOverrides = map[string]string{name: payload}
	default:
		return ov, fmt.Errorf("point invalido %q: use marker | body | url | query:<param> | header:<nome>", point)
	}
	return ov, nil
}

// isFuzzAnomaly marca uma resposta como interessante vs o baseline: status
// diferente, tamanho variou >20%, ou o payload refletiu no corpo.
func isFuzzAnomaly(orig *store.Request, status, respLen int, reflected bool) bool {
	if reflected {
		return true
	}
	if status != orig.Status {
		return true
	}
	base := orig.RespLen
	if base == 0 {
		return respLen != 0
	}
	delta := respLen - base
	if delta < 0 {
		delta = -delta
	}
	return float64(delta)/float64(base) > 0.2
}
