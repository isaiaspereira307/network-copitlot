// ponytail: testes chamam cli.NewRootCmd (mesmo wiring que main); pflag retem estado entre Execute
// ponytail: por isso cada runCmd invoca NewRootCmd fresh, sem cache
package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/cli"
	"github.com/isaias/network-copitlot/internal/config"
	"github.com/isaias/network-copitlot/internal/projects"
)

func TestE2E_V2Workspaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load()
	cfg.WorkspacePath = filepath.Join(dir, "ws")
	cfg.Save()
	repo := projects.NewRepo(cfg.WorkspacePath)
	active := projects.NewActiveState(repo, cfg)
	alPath := filepath.Join(dir, "audit.log")
	al, _ := audit.New(alPath)
	t.Cleanup(func() { al.Close() })

	// 1. criar projeto via CLI
	out := runCmd(t, active, repo, al, "project", "create", "--name", "HackerOne-X", "--type", "bugbounty")
	if !strings.Contains(out, "HackerOne-X") {
		t.Fatalf("project create: %s", out)
	}

	// 2. usar projeto
	out = runCmd(t, active, repo, al, "project", "use", "HackerOne-X")
	if !strings.Contains(out, "ativo") {
		t.Fatalf("project use: %s", out)
	}

	// 3. adicionar alvo com confirm
	out = runCmd(t, active, repo, al, "target", "add", "--host", "api.empresa.com", "--confirm")
	if !strings.Contains(out, "api.empresa.com") {
		t.Fatalf("target add: %s", out)
	}

	// 4. alvo sem confirm: erro
	if err := runCmdErr(t, active, repo, al, "target", "add", "--host", "evil.com"); err == nil {
		t.Fatal("expected error without --confirm")
	}

	// 5. usar alvo
	out = runCmd(t, active, repo, al, "target", "use", "api.empresa.com")
	if !strings.Contains(out, "ativo") {
		t.Fatalf("target use: %s", out)
	}

	// 6. verificar persistencia
	cfg2, _ := config.Load()
	if cfg2.ActiveProject != "HackerOne-X" || cfg2.ActiveTarget != "api.empresa.com" {
		t.Errorf("config not persisted: %+v", cfg2)
	}

	// 7. verificar filesystem
	mustExist(t, filepath.Join(cfg.WorkspacePath, "HackerOne-X", "meta.yaml"))
	mustExist(t, filepath.Join(cfg.WorkspacePath, "HackerOne-X", "targets", "api.empresa.com", "meta.yaml"))
	mustExist(t, filepath.Join(cfg.WorkspacePath, "HackerOne-X", "targets", "api.empresa.com", "requests.db"))

	// 8. verificar audit log
	data, _ := os.ReadFile(alPath)
	if !strings.Contains(string(data), "create") {
		t.Errorf("audit log missing create: %s", data)
	}
}

func runCmd(t *testing.T, active *projects.ActiveState, repo *projects.Repo, al *audit.Logger, args ...string) string {
	t.Helper()
	root := cli.NewRootCmd(active, repo, al)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("cmd %v: %v", args, err)
	}
	return buf.String()
}

func runCmdErr(t *testing.T, active *projects.ActiveState, repo *projects.Repo, al *audit.Logger, args ...string) error {
	t.Helper()
	root := cli.NewRootCmd(active, repo, al)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	return root.Execute()
}

func mustExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err != nil {
		t.Errorf("expected %s: %v", p, err)
	}
}
