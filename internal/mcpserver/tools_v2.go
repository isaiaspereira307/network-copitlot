package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/projects"
)

// toolFunc: assinatura comum de todas as tools v2.
type toolFunc func(ctx context.Context, args map[string]any) (string, error)

// toolRegistry: registry local usado pelos testes para invocar toolFunc sem
// subir stdio. O wiring real no mcp-go Server (s.mcp.AddTool) acontece na Task 15.
var toolRegistry = map[string]toolFunc{}

func registerV2Tools(s *Server) {
	// project tools (task 12)
	toolRegistry["create_project"] = s.toolCreateProject
	toolRegistry["list_projects"] = s.toolListProjects
	toolRegistry["set_active_project"] = s.toolSetActiveProject
	// target tools (task 13)
	toolRegistry["add_target"] = s.toolAddTarget
	toolRegistry["list_targets"] = s.toolListTargets
	toolRegistry["set_active_target"] = s.toolSetActiveTarget
	// context (task 14)
	toolRegistry["get_active_context"] = s.toolGetActiveContext
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

// stubs: toolGetActiveContext preenchido na task 14.
func (s *Server) toolGetActiveContext(ctx context.Context, args map[string]any) (string, error) {
	return "", fmt.Errorf("not implemented yet")
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
