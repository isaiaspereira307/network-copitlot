package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/config"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/spf13/cobra"
)

func setupCLI(t *testing.T) (*cobra.Command, *projects.ActiveState) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load()
	cfg.WorkspacePath = t.TempDir()
	cfg.Save()
	repo := projects.NewRepo(cfg.WorkspacePath)
	active := projects.NewActiveState(repo, cfg)
	al, _ := audit.New(filepath.Join(t.TempDir(), "audit.log"))
	t.Cleanup(func() { al.Close() })

	root := NewRootCmd(active, repo, al)
	return root, active
}

func captureRun(t *testing.T, cmd *cobra.Command, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	buf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs(append([]string{"project"}, args...))
	err = cmd.Execute()
	return buf.String(), errBuf.String(), err
}

func TestProjectCreate(t *testing.T) {
	root, _ := setupCLI(t)
	out, _, err := captureRun(t, root, "create", "--name", "P1", "--type", "bugbounty")
	if err != nil {
		t.Fatalf("err: %v stderr=%s", err, out)
	}
	if !strings.Contains(out, "P1") {
		t.Errorf("output missing P1: %s", out)
	}
}

func TestProjectList_Empty(t *testing.T) {
	root, _ := setupCLI(t)
	out, _, err := captureRun(t, root, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out), "nenhum") && out != "" {
		t.Logf("list output: %s", out)
	}
}

func TestProjectUse(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, _ = captureRun(t, root, "create", "--name", "P1", "--type", "bugbounty")
	_, _, err := captureRun(t, root, "use", "P1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestProjectUse_NotFound(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, err := captureRun(t, root, "use", "NOPE")
	if err == nil {
		t.Fatal("expected error")
	}
}
