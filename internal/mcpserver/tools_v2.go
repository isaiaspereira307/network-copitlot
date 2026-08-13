package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/store"
)

// toolFunc: assinatura comum de todas as tools v2.
type toolFunc func(ctx context.Context, args map[string]any) (string, error)

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
	}
	if proj != nil {
		out["active_project"] = proj.Name
		out["project_type"] = string(proj.Type)
	}
	if tgt != nil {
		out["active_target"] = tgt.Host
		st, err := s.openStoreForActiveTarget()
		if err == nil && st != nil {
			n, _ := st.Count()
			out["request_count"] = n
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
	argInt := func(k string) (int64, bool) {
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
	argStr := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	f := store.ListFilter{Limit: 50}
	if v, ok := argInt("limit"); ok {
		f.Limit = int(v)
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200 // clamp: token-frugal
	}
	if v, ok := argInt("offset"); ok {
		f.Offset = int(v)
	}
	if v, ok := argInt("status_filter"); ok {
		f.StatusFilter = int(v)
	}
	if v, ok := argInt("since_id"); ok {
		f.SinceID = v
	}
	f.MethodFilter = argStr("method_filter")
	f.HostFilter = argStr("host_filter")
	f.PathContains = argStr("path_contains")

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
