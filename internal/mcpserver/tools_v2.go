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

// stubs: preenchidos nas tasks 13 (targets) e 14 (context).
func (s *Server) toolAddTarget(ctx context.Context, args map[string]any) (string, error) {
	return "", fmt.Errorf("not implemented yet")
}
func (s *Server) toolListTargets(ctx context.Context, args map[string]any) (string, error) {
	return "", fmt.Errorf("not implemented yet")
}
func (s *Server) toolSetActiveTarget(ctx context.Context, args map[string]any) (string, error) {
	return "", fmt.Errorf("not implemented yet")
}
func (s *Server) toolGetActiveContext(ctx context.Context, args map[string]any) (string, error) {
	return "", fmt.Errorf("not implemented yet")
}
