package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/intruder"
	"github.com/isaiaspereira307/network-copitlot/internal/macro"
	"github.com/isaiaspereira307/network-copitlot/internal/proxy"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// maxIntruderCases limita o total de casos gerados por um intruder_start (safety
// valve contra produto cartesiano explosivo; PRD v3.0: cap p/ nao virar DoS).
const maxIntruderCases = 2000

func registerV5Tools(s *Server) {
	// intruder (v3.0)
	s.tools["intruder_start"] = s.toolIntruderStart
	s.tools["intruder_status"] = s.toolIntruderStatus
	s.tools["intruder_results"] = s.toolIntruderResults
	s.tools["intruder_cancel"] = s.toolIntruderCancel
	// macro / session handling (v3.0)
	s.tools["macro_record"] = s.toolMacroRecord
	s.tools["macro_play"] = s.toolMacroPlay
	s.tools["macro_list"] = s.toolMacroList
}

// ensureEngine inicializa o engine intruder apontando para o workspace do
// projeto ativo (resultados em workspace/<proj>/intruder/jobs).
func (s *Server) ensureEngine() *intruder.Engine {
	s.engInit.Do(func() {
		dir := ""
		if p, _ := s.active.Project(); p != nil {
			dir = p.Dir(s.repo.WorkspacePath())
		}
		s.engine = intruder.NewEngine(dir)
	})
	return s.engine
}

func (s *Server) ensureMacros() *macro.Manager {
	s.macInit.Do(func() {
		dir := ""
		if p, _ := s.active.Project(); p != nil {
			dir = p.Dir(s.repo.WorkspacePath())
		}
		s.macros = macro.NewManager(dir)
	})
	return s.macros
}

// toolIntruderStart lanca um job de fuzzing (assync) com os 4 attack types e
// posicoes/sets de payload. Retorna o job id; o progresso e consultado via
// intruder_status/intruder_results. Cada caso e reenviado sob scope guard.
func (s *Server) toolIntruderStart(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	baseID, ok := argInt(args, "base_request_id")
	if !ok || baseID <= 0 {
		return "", fmt.Errorf("base_request_id obrigatorio")
	}
	orig, err := st.Get(baseID)
	if err != nil {
		return "", fmt.Errorf("request base %d nao encontrado: %w", baseID, err)
	}

	attack := intruder.AttackType(argStr(args, "attack_type"))
	if !attack.Valid() {
		return "", fmt.Errorf("attack_type invalido: sniper|battering_ram|pitchfork|cluster_bomb")
	}

	// Posicoes de fuzz: strings do tipo url|body|query:<p>|header:<n>.
	posRaw, err := stringArr(args, "positions")
	if err != nil || len(posRaw) == 0 {
		return "", fmt.Errorf("positions obrigatorio: array de [url|body|query:<param>|header:<name>]")
	}
	positions := make([]intruder.Position, 0, len(posRaw))
	for _, p := range posRaw {
		pos, perr := intruder.ParsePosition(p)
		if perr != nil {
			return "", perr
		}
		positions = append(positions, pos)
	}

	// Payload sets: ou payload_sets (array de arrays) ou payload_set (builtin)
	// aplicado a todas as posicoes; ou payloads (array unico).
	sets, err := resolvePayloadSets(args, len(positions))
	if err != nil {
		return "", err
	}

	cases, err := intruder.Generate(attack, positions, sets)
	if err != nil {
		return "", err
	}
	if len(cases) > maxIntruderCases {
		return "", fmt.Errorf("intruder geraria %d casos (> %d): reduza payloads/positions ou use attack=sniper", len(cases), maxIntruderCases)
	}

	jobID := fmt.Sprintf("i%d", baseID) // simples e determinista p/ o alvo atual
	throttleRPS := 0.0
	if v, ok := argFloat(args, "throttle_rps"); ok && v > 0 {
		throttleRPS = v
	}

	// scope guard (reuso do proxy.New como nas demais tools)
	tgt, _ := s.active.Target()
	sc := proxy.New(tgt)
	scopeMatch := func(host string) bool { return sc.Matches(&url.URL{Host: host}) }

	// posicao -> injecao; guarda o indice para o replier
	posKinds := make([]string, len(positions))
	for i, p := range positions {
		posKinds[i] = p.Kind
	}

	job := s.ensureEngine().Run(jobID, attack, posKinds, baseID, cases, throttleRPS,
		func(caseIdx int, payloads []string) (intruder.CaseResult, error) {
			ov, oerr := applyPayloads(orig, positions, payloads)
			if oerr != nil {
				return intruder.CaseResult{Case: caseIdx}, oerr
			}
			res, rerr := st.Replay(baseID, ov, scopeMatch)
			if rerr != nil {
				return intruder.CaseResult{Case: caseIdx}, rerr
			}
			return intruder.CaseResult{Case: caseIdx, ReplayID: res.NewRequestID, Status: res.Status, RespLen: res.RespLen}, nil
		})

	out := map[string]any{"job_id": job.ID, "attack": job.Attack, "status": job.Status, "total_cases": job.TotalCases}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "intruder_start", Action: "start", Detail: map[string]any{
		"job_id": job.ID, "base": baseID, "attack": attack, "cases": job.TotalCases,
	}})
	return string(b), nil
}

func (s *Server) toolIntruderStatus(ctx context.Context, args map[string]any) (string, error) {
	jobID := argStr(args, "job_id")
	if jobID == "" {
		return "", fmt.Errorf("job_id obrigatorio")
	}
	eng := s.ensureEngine()
	out := map[string]any{"job_id": jobID}
	if snap, ok := eng.Snapshot(jobID); ok {
		out["status"] = snap.Status
		out["done"] = snap.Done
		out["total_cases"] = snap.TotalCases
		out["anomalies"] = snap.Anomalies
		if snap.Error != "" {
			out["error"] = snap.Error
		}
	} else {
		out["status"] = "unknown"
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (s *Server) toolIntruderResults(ctx context.Context, args map[string]any) (string, error) {
	jobID := argStr(args, "job_id")
	if jobID == "" {
		return "", fmt.Errorf("job_id obrigatorio")
	}
	grep := argStr(args, "grep")
	eng := s.ensureEngine()
	snap, ok := eng.Snapshot(jobID)
	if !ok {
		return "", fmt.Errorf("job %s nao encontrado", jobID)
	}
	results := snap.Results
	if snap.Status != intruder.StatusDone {
		out := map[string]any{"job_id": snap.ID, "status": snap.Status, "partial": true}
		b, _ := json.Marshal(out)
		return string(b), nil
	}
	// filtra por grep se fornecido
	filtered := results
	if grep != "" {
		filtered = nil
		for _, r := range results {
			if r.Err != "" {
				continue
			}
			// olha o corpo do novo request no store
			if st, err := s.openStoreForActiveTarget(); err == nil && st != nil {
				if rr, gErr := st.Get(r.ReplayID); gErr == nil && strings.Contains(string(rr.RespBody), grep) {
					filtered = append(filtered, r)
				}
			}
		}
	}
	// agregar por (status -> count) para economizar tokens, mais a lista de
	// anomalias completa
	agg := map[int]int{}
	for _, r := range results {
		agg[r.Status]++
	}
	out := map[string]any{
		"job_id":       snap.ID,
		"status":       snap.Status,
		"anomalies":    snap.Anomalies,
		"total":        len(results),
		"by_status":    agg,
		"anomalies_list": snap.AnomResults,
		"results":      filtered,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (s *Server) toolIntruderCancel(ctx context.Context, args map[string]any) (string, error) {
	jobID := argStr(args, "job_id")
	if jobID == "" {
		return "", fmt.Errorf("job_id obrigatorio")
	}
	if !s.ensureEngine().Cancel(jobID) {
		return "", fmt.Errorf("job %s nao encontrado", jobID)
	}
	return fmt.Sprintf("job %s cancelado (em andamento)", jobID), nil
}

// toolMacroRecord salva uma macro (cadeia de steps) sob um nome, com
// extractors opcionais. Retorna o macro id. A gravacao em si precisa do store
// ativo (para escolher requests do historico), mas aqui registramos o nome e
// aceitamos steps explicitos (espelho do macro_record do PRD).
func (s *Server) toolMacroRecord(ctx context.Context, args map[string]any) (string, error) {
	name := argStr(args, "name")
	if name == "" {
		return "", fmt.Errorf("name obrigatorio")
	}
	mac := &macro.Macro{Name: name}

	if rawSteps, ok := args["steps"].([]any); ok && len(rawSteps) > 0 {
		for i, item := range rawSteps {
			m, ok := item.(map[string]any)
			if !ok {
				return "", fmt.Errorf("steps[%d] invalido: esperado objeto", i)
			}
			step := macro.Step{
				Method: argStr(m, "method"),
				URL:    argStr(m, "url"),
				Body:   argStr(m, "body"),
			}
			if h, ok := m["headers"].(map[string]any); ok {
				step.Headers = map[string][]string{}
				for k, v := range h {
					if vs, ok := v.(string); ok {
						step.Headers[k] = []string{vs}
					}
				}
			}
			if ex, ok := m["extractors"].([]any); ok {
				for _, e := range ex {
					em, ok := e.(map[string]any)
					if !ok {
						continue
					}
					step.Extractors = append(step.Extractors, macro.Extractor{
						Name:    argStr(em, "name"),
						Pattern: argStr(em, "pattern"),
					})
				}
			}
			if step.Method == "" || step.URL == "" {
				return "", fmt.Errorf("steps[%d]: method e url obrigatorios", i)
			}
			mac.Steps = append(mac.Steps, step)
		}
	}
	if len(mac.Steps) == 0 {
		return "", fmt.Errorf("steps obrigatorio: array de {method, url, headers?, body?, extractors?}")
	}
	if err := s.ensureMacros().Save(mac); err != nil {
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "macro_record", Action: "record", Detail: map[string]any{"name": name, "steps": len(mac.Steps)}})
	out := map[string]any{"macro_id": mac.ID, "name": name, "steps": len(mac.Steps)}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// toolMacroPlay executa uma macro, mantendo a sessao (variaveis extraidas via
// regex nos extractors) e substituindo {var} nos steps subsequentes. Reusa o
// store.Replay sob scope guard por step; cada step vira um novo request.
func (s *Server) toolMacroPlay(ctx context.Context, args map[string]any) (string, error) {
	name := argStr(args, "name")
	if name == "" {
		return "", fmt.Errorf("name obrigatorio (macro a executar)")
	}
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	tgt, _ := s.active.Target()
	sc := proxy.New(tgt)
	scopeMatch := func(host string) bool { return sc.Matches(&url.URL{Host: host}) }

	res, err := s.ensureMacros().Play(name, argStr(args, "session_id"),
		func(step macro.Step, vars map[string]string) ([]byte, int, error) {
			// substitue variaveis no URL e body
			method := step.Method
			u := macro.Substitute(step.URL, vars)
			body := macro.Substitute(step.Body, vars)
			// monta headers com variaveis
			headers := map[string]string{}
			for k, vs := range step.Headers {
				if len(vs) > 0 {
					headers[k] = macro.Substitute(vs[0], vars)
				}
			}
			ov := store.ReplayOverrides{
				MethodOverride: method,
				URLOverride:    u,
				HeaderOverrides: headers,
			}
			if body != "" {
				ov.BodyOverride = []byte(body)
			}
			parsed, _ := url.Parse(u)
			if parsed.Host == "" {
				return nil, 0, fmt.Errorf("macro step sem host valido: %q", u)
			}
			if !scopeMatch(parsed.Hostname()) {
				return nil, 0, store.ErrOutOfScope
			}
			// precisa de um request base para Replay (o store.Replay exige um id);
			// criamos um request sintetico aqui replicando o corpo/headers manuais.
			// Nota: macro steps sao requests novos, nao replays de capturados.
			return execMacroStep(st, method, u, headers, body)
		})
	if err != nil {
		return "", fmt.Errorf("macro %s: %w", name, err)
	}
	s.audit.Log(audit.Event{Tool: "macro_play", Action: "play", Detail: map[string]any{
		"name": name, "session": res.MacroID, "steps": res.StepsRun, "status": res.Status,
	}})
	out := map[string]any{
		"name": name, "session_id": res.MacroID, "steps_run": res.StepsRun,
		"last_status": res.Status, "vars": res.Vars,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// execMacroStep executa um request HTTP arbitrario (macro step) via
// store.Replay reutilizando um request base sintetico. Como store.Replay exige
// um id, usamos o request mais recente do store como base para headers/URL, e
// aplicamos os overrides. Falha se o request base nao existir.
// Alternativa mais limpa (PRD v2.1 send_custom_request) vira na task v2.1.
func execMacroStep(st store.Store, method, rawURL string, headers map[string]string, body string) ([]byte, int, error) {
	// pega o request mais recente como base (pra ter um id valido p/ Replay)
	baseID, err := latestID(st)
	if err != nil {
		return nil, 0, fmt.Errorf("sem request base para macro step: %w", err)
	}
	ov := store.ReplayOverrides{
		MethodOverride: method,
		URLOverride:    rawURL,
		HeaderOverrides: headers,
	}
	if body != "" {
		ov.BodyOverride = []byte(body)
	}
	res, err := st.Replay(baseID, ov, func(string) bool { return true })
	if err != nil {
		return nil, 0, err
	}
	r, err := st.Get(res.NewRequestID)
	if err != nil {
		return nil, 0, err
	}
	return r.RespBody, res.Status, nil
}

func latestID(st store.Store) (int64, error) {
	list, err := st.List(store.ListFilter{Limit: 1})
	if err != nil {
		return 0, err
	}
	if len(list) == 0 {
		return 0, fmt.Errorf("nenhum request capturado")
	}
	return list[0].ID, nil
}

func (s *Server) toolMacroList(ctx context.Context, args map[string]any) (string, error) {
	names, err := s.ensureMacros().List()
	if err != nil {
		return "", err
	}
	out := map[string]any{"count": len(names), "macros": names}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// --- helpers ---------------------------------------------------------------

// stringArr extrai um array de strings.
func stringArr(args map[string]any, k string) ([]string, error) {
	switch v := args[k].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out, nil
	case []string:
		return v, nil
	}
	return nil, fmt.Errorf("%s deve ser um array de strings", k)
}

// argFloat extrai um float (MCP deserializa numeros como float64).
func argFloat(args map[string]any, k string) (float64, bool) {
	switch v := args[k].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

// resolvePayloadSets monta os sets de payload conforme os parametros.
func resolvePayloadSets(args map[string]any, order int) ([][]string, error) {
	// priority: payload_sets (já estruturado)
	if raw, ok := args["payload_sets"].([]any); ok && len(raw) > 0 {
		sets := make([][]string, 0, len(raw))
		for _, setRaw := range raw {
			items, ok := setRaw.([]any)
			if !ok {
				return nil, fmt.Errorf("payload_sets deve ser array de arrays de string")
			}
			set := make([]string, 0, len(items))
			for _, it := range items {
				if s, ok := it.(string); ok {
					set = append(set, s)
				}
			}
			if len(set) == 0 {
				return nil, fmt.Errorf("payload set vazio")
			}
			sets = append(sets, set)
		}
		if len(sets) != order {
			return nil, fmt.Errorf("payload_sets len (%d) != positions (%d)", len(sets), order)
		}
		return sets, nil
	}
	// payload_set builtin (aplica mesma ao corpus em cada posicao)
	if pset := argStr(args, "payload_set"); pset != "" {
		set, ok := builtinPayloadSets[strings.ToLower(pset)]
		if !ok {
			return nil, fmt.Errorf("payload_set desconhecido: %s", pset)
		}
		sets := make([][]string, order)
		for i := range sets {
			sets[i] = set
		}
		return sets, nil
	}
	// payloads (array unico) aplicado a cada posicao
	if pl, err := stringArr(args, "payloads"); err == nil && len(pl) > 0 {
		sets := make([][]string, order)
		for i := range sets {
			sets[i] = pl
		}
		return sets, nil
	}
	return nil, fmt.Errorf("faltando payload: payload_sets | payload_set | payloads")
}

// applyPayloads injeta os payloads nas posicoes do request base, retornando os
// ReplayOverrides correspondentes.
func applyPayloads(orig *store.Request, positions []intruder.Position, payloads []string) (store.ReplayOverrides, error) {
	var ov store.ReplayOverrides
	// Caso multi-pos: construimos URL/body a partir das mutacoes linha a linha.
	// Simplificacao: aplicamos payloads apenas na primeira posicao alteravel de
	// cada tipo (url/body/query/header); suporte multi-pos completo vira em
	// refinamento. Para o uso primario (fuzzing de 1-2 params) cobre o caso.
	for i, p := range positions {
		if i >= len(payloads) {
			break
		}
		pl := payloads[i]
		if pl == "" {
			continue // manter original (sniper deixa as outras posicoes intactas)
		}
		switch {
		case p.Kind == "url":
			ov.URLOverride = pl
		case p.Kind == "body":
			ov.BodyOverride = []byte(pl)
		case strings.HasPrefix(p.Kind, "query:"):
			name := strings.TrimPrefix(p.Kind, "query:")
			u, err := url.Parse(orig.URL)
			if err != nil {
				return ov, err
			}
			q := u.Query()
			q.Set(name, pl)
			u.RawQuery = q.Encode()
			ov.URLOverride = u.String()
		case strings.HasPrefix(p.Kind, "header:"):
			name := strings.TrimPrefix(p.Kind, "header:")
			if ov.HeaderOverrides == nil {
				ov.HeaderOverrides = map[string]string{}
			}
			ov.HeaderOverrides[name] = pl
		}
	}
	return ov, nil
}
