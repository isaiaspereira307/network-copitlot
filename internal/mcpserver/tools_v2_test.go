package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/config"
	"github.com/isaias/network-copitlot/internal/projects"
)

func newTestServer(t *testing.T) (*Server, *projects.ActiveState) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load()
	cfg.WorkspacePath = t.TempDir()
	cfg.Save()
	repo := projects.NewRepo(cfg.WorkspacePath)
	active := projects.NewActiveState(repo, cfg)
	auditDir := t.TempDir()
	al, _ := audit.New(filepath.Join(auditDir, "audit.log"))
	t.Cleanup(func() { al.Close() })
	s := New(active, repo, al)
	registerV2Tools(s)
	return s, active
}

func TestCreateProjectTool(t *testing.T) {
	s, _ := newTestServer(t)
	out, err := callTool(t, s, "create_project", map[string]any{
		"name": "HackerOne-X",
		"type": "bugbounty",
	})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	if out == "" {
		t.Fatal("empty output")
	}
	repo := projects.NewRepo(getWorkspace(t))
	p, err := repo.LoadProject("HackerOne-X")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Type != projects.ProjectBugBounty {
		t.Errorf("type = %q", p.Type)
	}
}

func TestListProjectsTool(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "A", "type": "pentest"})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	_, err = callTool(t, s, "create_project", map[string]any{"name": "B", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	out, err := callTool(t, s, "list_projects", map[string]any{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestSetActiveProjectTool(t *testing.T) {
	s, active := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P1", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := callTool(t, s, "set_active_project", map[string]any{"name": "P1"})
	if err != nil {
		t.Fatalf("set_active: %v", err)
	}
	if out == "" {
		t.Fatal("empty")
	}
	if p, _ := active.Project(); p == nil {
		t.Fatal("active project not set")
	}
}

func TestSetActiveProject_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	out, err := callTool(t, s, "set_active_project", map[string]any{"name": "NOPE"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if out != "" {
		t.Errorf("expected empty out on error, got %q", out)
	}
}

func TestAddTargetTool_RequiresConfirmation(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	_, err = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	if err != nil {
		t.Fatalf("set_active_project: %v", err)
	}
	// sem confirmed: true -> erro
	_, err = toolRegistry["add_target"](context.Background(), map[string]any{"host": "x.com"})
	if err == nil {
		t.Fatal("expected error when confirmation missing")
	}
}

func TestAddTargetTool_WithConfirmation(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	_, err = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	if err != nil {
		t.Fatalf("set_active_project: %v", err)
	}
	out, err := callTool(t, s, "add_target", map[string]any{
		"host":      "api.empresa.com",
		"confirmed": true,
	})
	if err != nil {
		t.Fatalf("add_target: %v", err)
	}
	if out == "" {
		t.Fatal("empty")
	}
	// verifica persistencia
	repo := projects.NewRepo(getWorkspace(t))
	tgt, err := repo.LoadTarget("P", "api.empresa.com")
	if err != nil {
		t.Fatalf("load target: %v", err)
	}
	if tgt.Host != "api.empresa.com" {
		t.Errorf("host = %q", tgt.Host)
	}
}

func TestAddTargetTool_NoActiveProject(t *testing.T) {
	_, _ = newTestServer(t)
	_, err := toolRegistry["add_target"](context.Background(), map[string]any{"host": "x.com", "confirmed": true})
	if err == nil {
		t.Fatal("expected error: no active project")
	}
}

func TestListTargetsTool(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	_, err = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	if err != nil {
		t.Fatalf("set_active_project: %v", err)
	}
	_, err = callTool(t, s, "add_target", map[string]any{"host": "a.com", "confirmed": true})
	if err != nil {
		t.Fatalf("add_target a: %v", err)
	}
	_, err = callTool(t, s, "add_target", map[string]any{"host": "b.com", "confirmed": true})
	if err != nil {
		t.Fatalf("add_target b: %v", err)
	}
	out, err := callTool(t, s, "list_targets", map[string]any{})
	if err != nil {
		t.Fatalf("list_targets: %v", err)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestSetActiveTargetTool(t *testing.T) {
	s, active := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	_, err = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	if err != nil {
		t.Fatalf("set_active_project: %v", err)
	}
	_, err = callTool(t, s, "add_target", map[string]any{"host": "x.com", "confirmed": true})
	if err != nil {
		t.Fatalf("add_target: %v", err)
	}
	_, err = callTool(t, s, "set_active_target", map[string]any{"host": "x.com"})
	if err != nil {
		t.Fatalf("set_active_target: %v", err)
	}
	tgt, _ := active.Target()
	if tgt == nil || tgt.Host != "x.com" {
		t.Errorf("active target not set: %+v", tgt)
	}
}

// callTool: retorna (out, err) para que testes de erro e sucesso compartilhem
// o mesmo helper. Variante do brief (que so retornava out e fazia Fatalf).
func callTool(t *testing.T, s *Server, name string, args map[string]any) (string, error) {
	t.Helper()
	fn, ok := toolRegistry[name]
	if !ok {
		t.Fatalf("tool %s not registered", name)
	}
	return fn(context.Background(), args)
}

func getWorkspace(t *testing.T) string {
	t.Helper()
	cfg, _ := config.Load()
	return cfg.WorkspacePath
}

var _ = time.Now
