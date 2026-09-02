package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/isaiaspereira307/network-copitlot/internal/proxy"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// toolFunc: assinatura comum de todas as tools v2.
type toolFunc func(ctx context.Context, args map[string]any) (string, error)

// argInt extrai inteiro de args (MCP deserializa numeros como float64).
func argInt(args map[string]any, k string) (int64, bool) {
	switch v := args[k].(type) {
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

func argStr(args map[string]any, k string) string {
	v, _ := args[k].(string)
	return v
}

func registerV2Tools(s *Server) {
	// project tools (task 12)
	s.tools["create_project"] = s.toolCreateProject
	s.tools["list_projects"] = s.toolListProjects
	s.tools["set_active_project"] = s.toolSetActiveProject
	// target tools (task 13)
	s.tools["add_target"] = s.toolAddTarget
	s.tools["list_targets"] = s.toolListTargets
	s.tools["set_active_target"] = s.toolSetActiveTarget
	// context (task 14)
	s.tools["get_active_context"] = s.toolGetActiveContext
	// registrar (task 8)
	s.tools["list_requests"] = s.toolListRequests
	// registrar (task 9)
	s.tools["get_request_detail"] = s.toolGetRequestDetail
	// registrar (task 10)
	s.tools["search_bodies"] = s.toolSearchBodies
	// registrar (task 11)
	s.tools["replay_request"] = s.toolReplayRequest
	// registrar (task 12)
	s.tools["set_scope"] = s.toolSetScope
}

func (s *Server) toolCreateProject(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	typ, _ := args["type"].(string)
	program, _ := args["program"].(string)
	platform, _ := args["platform"].(string)
	if name == "" || typ == "" {
		s.audit.Log(audit.Event{Tool: "create_project", Action: "error", Detail: map[string]any{"err": "name e type sao obrigatorios"}})
		return "", fmt.Errorf("name e type sao obrigatorios")
	}
	p := &projects.Project{
		Name:      name,
		Type:      projects.ProjectType(typ),
		Program:   program,
		Platform:  platform,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.repo.CreateProject(p); err != nil {
		s.audit.Log(audit.Event{Tool: "create_project", Action: "error", Detail: map[string]any{"name": name, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "create_project", Action: "create", Detail: map[string]any{"name": name}})
	return fmt.Sprintf("projeto criado: %s", name), nil
}

func (s *Server) toolListProjects(ctx context.Context, args map[string]any) (string, error) {
	list, err := s.repo.ListProjects()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "list_projects", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	out, _ := json.Marshal(list)
	s.audit.Log(audit.Event{Tool: "list_projects", Action: "list", Detail: map[string]any{"count": len(list)}})
	return string(out), nil
}

func (s *Server) toolSetActiveProject(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	if name == "" {
		s.audit.Log(audit.Event{Tool: "set_active_project", Action: "error", Detail: map[string]any{"err": "name obrigatorio"}})
		return "", fmt.Errorf("name obrigatorio")
	}
	if err := s.active.SetProject(name); err != nil {
		s.audit.Log(audit.Event{Tool: "set_active_project", Action: "error", Detail: map[string]any{"name": name, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "set_active_project", Action: "set", Detail: map[string]any{"name": name}})
	return fmt.Sprintf("projeto ativo: %s", name), nil
}

func (s *Server) toolGetActiveContext(ctx context.Context, args map[string]any) (string, error) {
	proj, _ := s.active.Project()
	tgt, _ := s.active.Target()
	out := map[string]any{
		"active_project": "",
		"active_target":  "",
		"request_count":  0,
		"status_counts":  map[int]int{},
		"top_hosts":      []map[string]any{},
		"endpoints":      0,
		"scope_defined":  false,
	}
	if proj != nil {
		out["active_project"] = proj.Name
		out["project_type"] = string(proj.Type)
	}
	if tgt != nil {
		out["active_target"] = tgt.Host
		out["scope_defined"] = len(tgt.InScopePatterns) > 0
		st, err := s.openStoreForActiveTarget()
		if err == nil && st != nil {
			n, _ := st.Count()
			out["request_count"] = n
			sums, err := st.List(store.ListFilter{Limit: 100000}) // ponytail: scan em memoria, SQL GROUP BY se >50k requests
			if err == nil {
				statusCounts := map[int]int{}
				hostCounts := map[string]int{}
				for _, r := range sums {
					statusCounts[r.Status]++
					if u, e := url.Parse(r.URL); e == nil && u.Host != "" {
						hostCounts[u.Host]++
					}
				}
				out["status_counts"] = statusCounts
				// top 5 hosts por count desc (sort estavel por host p/ determinismo)
				hosts := make([]string, 0, len(hostCounts))
				for h := range hostCounts {
					hosts = append(hosts, h)
				}
				sort.Slice(hosts, func(i, j int) bool {
					if hostCounts[hosts[i]] != hostCounts[hosts[j]] {
						return hostCounts[hosts[i]] > hostCounts[hosts[j]]
					}
					return hosts[i] < hosts[j]
				})
				if len(hosts) > 5 {
					hosts = hosts[:5]
				}
				top := make([]map[string]any, 0, len(hosts))
				for _, h := range hosts {
					top = append(top, map[string]any{"host": h, "count": hostCounts[h]})
				}
				out["top_hosts"] = top
			}
			if eps, err := st.ListEndpoints(); err == nil {
				out["endpoints"] = len(eps)
			}
		}
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// toolListRequests lista requests do alvo ativo (apenas resumo, nunca corpos).
// Paginado por recencia (id DESC). limit default 50, clamp max 200 (token-frugal).
func (s *Server) toolListRequests(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "list_requests", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	f := store.ListFilter{Limit: 50}
	if v, ok := argInt(args, "limit"); ok {
		f.Limit = int(v)
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200 // clamp: token-frugal
	}
	if v, ok := argInt(args, "offset"); ok {
		f.Offset = int(v)
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	if v, ok := argInt(args, "status_filter"); ok {
		f.StatusFilter = int(v)
	}
	if v, ok := argInt(args, "since_id"); ok {
		f.SinceID = v
	}
	f.MethodFilter = argStr(args, "method_filter")
	f.HostFilter = argStr(args, "host_filter")
	f.PathContains = argStr(args, "path_contains")

	list, err := st.List(f)
	if err != nil {
		s.audit.Log(audit.Event{Tool: "list_requests", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	// ponytail: map manual p/ keys minusculas (id, ts, method, url, status, resp_len);
	// RequestSummary sem json tags. Corpos jamais saem aqui.
	summaries := make([]map[string]any, 0, len(list))
	for _, sm := range list {
		summaries = append(summaries, map[string]any{
			"id":       sm.ID,
			"ts":       sm.Ts,
			"method":   sm.Method,
			"url":      sm.URL,
			"status":   sm.Status,
			"resp_len": sm.RespLen,
		})
	}
	out := map[string]any{"count": len(summaries), "requests": summaries}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "list_requests", Action: "list", Detail: map[string]any{"count": len(summaries)}})
	return string(b), nil
}

// toolGetRequestDetail retorna detalhe de um request capturado. Corpos sao
// truncados por default (max_body_bytes=8192); flags body_truncated + total_len
// sinalizam ao AI que o body foi cortado. use body_range p/ paginar.
func (s *Server) toolGetRequestDetail(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "get_request_detail", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	id, ok := argInt(args, "id")
	if !ok || id <= 0 {
		s.audit.Log(audit.Event{Tool: "get_request_detail", Action: "error", Detail: map[string]any{"err": "id obrigatorio"}})
		return "", fmt.Errorf("id obrigatorio: passe o id do request (ex: 42)")
	}
	include := argStr(args, "include")
	if include == "" {
		include = "headers"
	}
	switch include {
	case "headers", "body", "all":
	default:
		s.audit.Log(audit.Event{Tool: "get_request_detail", Action: "error", Detail: map[string]any{"err": "include invalido"}})
		return "", fmt.Errorf("include invalido: headers|body|all")
	}
	maxBody := 8192
	if v, ok := argInt(args, "max_body_bytes"); ok {
		maxBody = int(v)
	}
	d, err := st.GetDetail(id, include, maxBody, argStr(args, "body_range"))
	if err != nil {
		s.audit.Log(audit.Event{Tool: "get_request_detail", Action: "error", Detail: map[string]any{"id": id, "err": err.Error()}})
		return "", err
	}
	out := map[string]any{
		"id":                  d.ID,
		"ts":                  d.Ts,
		"method":              d.Method,
		"url":                 d.URL,
		"req_headers":         d.ReqHeaders,
		"req_body":            bodyString(d.ReqBody),
		"status":              d.Status,
		"resp_headers":        d.RespHeaders,
		"resp_body":           bodyString(d.RespBody),
		"resp_len":            d.RespLen,
		"req_body_truncated":  d.ReqBodyTruncated,
		"resp_body_truncated": d.RespBodyTruncated,
		"req_total_len":       d.ReqTotalLen,
		"resp_total_len":      d.RespTotalLen,
	}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "get_request_detail", Action: "get", Detail: map[string]any{"id": id, "include": include, "req_truncated": d.ReqBodyTruncated, "resp_truncated": d.RespBodyTruncated}})
	return string(b), nil
}

// bodyString: corpo em string quando UTF-8 (legivel p/ AI), base64 se binario.
func bodyString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// toolSearchBodies busca `query` (regex se compila, senao substring) nos corpos
// req/resp do alvo ativo. Retorna um snippet curto (+/-80 chars) por hit — o
// corpo completo JAMAIS sai aqui (token-frugal). Follow-up: get_request_detail.
func (s *Server) toolSearchBodies(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "search_bodies", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	query := argStr(args, "query")
	if query == "" {
		s.audit.Log(audit.Event{Tool: "search_bodies", Action: "error", Detail: map[string]any{"err": "query obrigatoria"}})
		return "", fmt.Errorf("query obrigatoria: padrao a buscar nos corpos (regex ou substring)")
	}
	scope := argStr(args, "scope")
	if scope == "" {
		scope = "both"
	}
	switch scope {
	case "req", "resp", "both":
	default:
		s.audit.Log(audit.Event{Tool: "search_bodies", Action: "error", Detail: map[string]any{"err": "scope invalido"}})
		return "", fmt.Errorf("scope invalido: req|resp|both")
	}
	limit := int64(30)
	if v, ok := argInt(args, "limit"); ok {
		limit = v
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100 // clamp: token-frugal
	}
	matches, err := st.SearchBodies(query, scope, int(limit))
	if err != nil {
		s.audit.Log(audit.Event{Tool: "search_bodies", Action: "error", Detail: map[string]any{"query": query, "err": err.Error()}})
		return "", err
	}
	// ponytail: map manual p/ keys minusculas; BodyMatch sem json tags.
	hits := make([]map[string]any, 0, len(matches))
	for _, m := range matches {
		hits = append(hits, map[string]any{
			"id":      m.ID,
			"url":     m.URL,
			"snippet": m.MatchSnippet,
		})
	}
	out := map[string]any{"count": len(hits), "query": query, "scope": scope, "matches": hits}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "search_bodies", Action: "search", Detail: map[string]any{"query": query, "scope": scope, "count": len(hits)}})
	return string(b), nil
}

// toolReplayRequest re-executa um request capturado (id) contra o host final,
// sempre validado pelo scope guard do alvo ativo (security pillar). Overrides
// opcionais: url, method, headers, body, follow_redirects. Resultado (novo id,
// status, resp_len) e persistido como novo request; corpos jamais retornados.
func (s *Server) toolReplayRequest(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "replay_request", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	id, ok := argInt(args, "id")
	if !ok || id <= 0 {
		s.audit.Log(audit.Event{Tool: "replay_request", Action: "error", Detail: map[string]any{"err": "id obrigatorio"}})
		return "", fmt.Errorf("id obrigatorio: passe o id do request (ex: 42)")
	}
	tgt, err := s.active.Target()
	if err != nil || tgt == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	// Scope guard construido do target ATIVO; store.Replay aplica ao host final
	// (pos override) e a cada redirect. Replay aborta se for fora de escopo.
	sc := proxy.New(tgt)
	scopeMatch := func(host string) bool { return sc.Matches(&url.URL{Host: host}) }

	ov := store.ReplayOverrides{
		URLOverride:    argStr(args, "url"),
		MethodOverride: argStr(args, "method"),
	}
	if v, ok := args["follow_redirects"].(bool); ok {
		ov.FollowRedirects = v
	}
	if hm, ok := args["headers"].(map[string]any); ok && len(hm) > 0 {
		ov.HeaderOverrides = make(map[string]string, len(hm))
		for k, v := range hm {
			if vs, ok := v.(string); ok {
				ov.HeaderOverrides[k] = vs
			}
		}
	}
	if b, ok := args["body"].(string); ok {
		ov.BodyOverride = []byte(b)
	}

	res, err := st.Replay(id, ov, scopeMatch)
	if err != nil {
		if errors.Is(err, store.ErrOutOfScope) {
			host := blockedHost(st, id, ov.URLOverride)
			s.audit.Log(audit.Event{Tool: "replay_request", Action: "error", Detail: map[string]any{"id": id, "host": host, "err": "out of scope"}})
			return "", fmt.Errorf("fora do escopo: replay bloqueado para host %s", host)
		}
		s.audit.Log(audit.Event{Tool: "replay_request", Action: "error", Detail: map[string]any{"id": id, "err": err.Error()}})
		return "", err
	}
	out := map[string]any{
		"new_request_id": res.NewRequestID,
		"status":         res.Status,
		"resp_len":       res.RespLen,
	}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "replay_request", Action: "replay", Detail: map[string]any{"id": id, "new_id": res.NewRequestID, "status": res.Status}})
	return string(b), nil
}

// blockedHost extrai o host efetivo (override vence) para citar na mensagem de
// out-of-scope. Melhor esforco: falha de parse -> vazio.
func blockedHost(st store.Store, id int64, override string) string {
	raw := override
	if raw == "" {
		if orig, err := st.Get(id); err == nil {
			raw = orig.URL
		}
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return ""
}

// toolSetScope persiste in_scope do alvo ativo no meta.yaml (read-modify-write
// via repo.SetScope). OutOfScopePatterns existentes sao preservados — a tool
// so toca in_scope. O proxy vivo capta a mudanca no proximo request via
// mtime-check do meta.yaml (processos separados, sem IPC).
func (s *Server) toolSetScope(ctx context.Context, args map[string]any) (string, error) {
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return "", fmt.Errorf("nenhum projeto ativo")
	}
	tgt, err := s.active.Target()
	if err != nil || tgt == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	raw, ok := args["in_scope"]
	if !ok {
		s.audit.Log(audit.Event{Tool: "set_scope", Action: "error", Detail: map[string]any{"err": "in_scope obrigatorio"}})
		return "", fmt.Errorf("in_scope obrigatorio: array de padroes (ex: [\"*.corp\"])")
	}
	inScope, _ := raw.([]any)
	pats := make([]string, 0, len(inScope))
	for _, x := range inScope {
		if str, ok := x.(string); ok && str != "" {
			pats = append(pats, str)
		}
	}
	if err := s.repo.SetScope(proj.Name, tgt.Host, pats, tgt.OutOfScopePatterns); err != nil {
		s.audit.Log(audit.Event{Tool: "set_scope", Action: "error", Detail: map[string]any{"host": tgt.Host, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "set_scope", Action: "set", Detail: map[string]any{"host": tgt.Host, "in_scope": pats}})
	return fmt.Sprintf("escopo do alvo %s atualizado: in_scope=%v (out_of_scope preservado)", tgt.Host, pats), nil
}

// toolAddTarget: requer confirmed=true do cliente MCP (PRD §5). Server NAO
// exibe prompt de confirmacao — apenas valida o parametro. Cliente (Claude) eh
// responsavel por perguntar "tem autorizacao?" antes de chamar.
func (s *Server) toolAddTarget(ctx context.Context, args map[string]any) (string, error) {
	host, _ := args["host"].(string)
	confirmed, _ := args["confirmed"].(bool)
	if host == "" {
		s.audit.Log(audit.Event{Tool: "add_target", Action: "error", Detail: map[string]any{"err": "host obrigatorio"}})
		return "", fmt.Errorf("host obrigatorio")
	}
	if !confirmed {
		s.audit.Log(audit.Event{Tool: "add_target", Action: "error", Detail: map[string]any{"host": host, "err": "confirmacao ausente"}})
		return "", fmt.Errorf("cliente deve confirmar autorizacao: passe confirmed=true (PRD §5)")
	}
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return "", fmt.Errorf("nenhum projeto ativo")
	}
	inScope, _ := args["in_scope"].([]any)
	outScope, _ := args["out_of_scope"].([]any)
	toStr := func(xs []any) []string {
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			if str, ok := x.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	tgt := &projects.Target{
		Host:               host,
		InScopePatterns:    toStr(inScope),
		OutOfScopePatterns: toStr(outScope),
		CreatedAt:          time.Now().UTC(),
	}
	if err := s.repo.AddTarget(proj.Name, tgt); err != nil {
		s.audit.Log(audit.Event{Tool: "add_target", Action: "error", Detail: map[string]any{"host": host, "project": proj.Name, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "add_target", Action: "add", Detail: map[string]any{"host": host, "project": proj.Name}})
	return fmt.Sprintf("alvo adicionado: %s/%s", proj.Name, host), nil
}

func (s *Server) toolListTargets(ctx context.Context, args map[string]any) (string, error) {
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return "", fmt.Errorf("nenhum projeto ativo")
	}
	list, err := s.repo.ListTargets(proj.Name)
	if err != nil {
		s.audit.Log(audit.Event{Tool: "list_targets", Action: "error", Detail: map[string]any{"project": proj.Name, "err": err.Error()}})
		return "", err
	}
	out, _ := json.Marshal(list)
	s.audit.Log(audit.Event{Tool: "list_targets", Action: "list", Detail: map[string]any{"project": proj.Name, "count": len(list)}})
	return string(out), nil
}

func (s *Server) toolSetActiveTarget(ctx context.Context, args map[string]any) (string, error) {
	host, _ := args["host"].(string)
	if host == "" {
		s.audit.Log(audit.Event{Tool: "set_active_target", Action: "error", Detail: map[string]any{"err": "host obrigatorio"}})
		return "", fmt.Errorf("host obrigatorio")
	}
	if err := s.active.SetTarget(host); err != nil {
		s.audit.Log(audit.Event{Tool: "set_active_target", Action: "error", Detail: map[string]any{"host": host, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "set_active_target", Action: "set", Detail: map[string]any{"host": host}})
	return fmt.Sprintf("alvo ativo: %s", host), nil
}
