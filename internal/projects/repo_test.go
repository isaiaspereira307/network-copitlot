package projects

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func loadFile(p string) ([]byte, error) { return os.ReadFile(p) }

func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	return NewRepo(dir)
}

func TestRepo_CreateLoadProject(t *testing.T) {
	r := newTestRepo(t)
	p := &Project{
		Name:      "HackerOne-EMPRESA",
		Type:      ProjectBugBounty,
		Program:   "EMPRESA",
		Platform:  "hackerone",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := r.CreateProject(p); err != nil {
		t.Fatalf("create: %v", err)
	}

	loaded, err := r.LoadProject("HackerOne-EMPRESA")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Type != ProjectBugBounty {
		t.Errorf("type = %q, want bugbounty", loaded.Type)
	}
	if loaded.Program != "EMPRESA" {
		t.Errorf("program = %q, want EMPRESA", loaded.Program)
	}
}

func TestRepo_CreateProject_Duplicate(t *testing.T) {
	r := newTestRepo(t)
	p := &Project{Name: "P", Type: ProjectBugBounty, CreatedAt: time.Now()}
	if err := r.CreateProject(p); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := r.CreateProject(p); err == nil {
		t.Fatal("expected error on duplicate")
	}
}

func TestRepo_ListProjects_Empty(t *testing.T) {
	r := newTestRepo(t)
	got, err := r.ListProjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestRepo_ListProjects_Multiple(t *testing.T) {
	r := newTestRepo(t)
	now := time.Now()
	for _, n := range []string{"A", "B", "C"} {
		if err := r.CreateProject(&Project{Name: n, Type: ProjectPentest, CreatedAt: now}); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
	}
	got, err := r.ListProjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
}

func TestRepo_AddLoadTarget(t *testing.T) {
	r := newTestRepo(t)
	if err := r.CreateProject(&Project{Name: "P", Type: ProjectBugBounty, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	tgt := &Target{
		Host:      "api.empresa.com",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := r.AddTarget("P", tgt); err != nil {
		t.Fatalf("add: %v", err)
	}
	loaded, err := r.LoadTarget("P", "api.empresa.com")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Host != "api.empresa.com" {
		t.Errorf("host = %q", loaded.Host)
	}
}

func TestRepo_AddTarget_ProjectMissing(t *testing.T) {
	r := newTestRepo(t)
	err := r.AddTarget("NOPE", &Target{Host: "x.com", CreatedAt: time.Now()})
	if err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestRepo_ListTargets_Empty(t *testing.T) {
	r := newTestRepo(t)
	if err := r.CreateProject(&Project{Name: "P", Type: ProjectBugBounty, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListTargets("P")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestRepo_ProjectDir_Correct(t *testing.T) {
	r := newTestRepo(t)
	p := &Project{Name: "P", Type: ProjectBugBounty, CreatedAt: time.Now()}
	if err := r.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(r.WorkspacePath(), "P", "meta.yaml")
	if _, err := loadFile(want); err != nil {
		t.Errorf("meta.yaml not at expected path: %v", err)
	}
}

func TestRepo_LoadTarget_PreservesIOError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0000 does not block owner access on windows")
	}
	r := newTestRepo(t)
	if err := r.CreateProject(&Project{Name: "P", Type: ProjectBugBounty, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	tgt := &Target{Host: "api.empresa.com", CreatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := r.AddTarget("P", tgt); err != nil {
		t.Fatalf("add: %v", err)
	}
	dir := filepath.Join(r.WorkspacePath(), "P", "targets", "api.empresa.com")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := r.LoadTarget("P", "api.empresa.com")
	if err == nil {
		t.Fatal("expected error from unreadable target dir, got nil")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ErrNotExist must not mask IO error; got %v", err)
	}
	if strings.Contains(err.Error(), "nao encontrado") {
		t.Fatalf("error must preserve underlying IO cause, not wrap as 'nao encontrado'; got %v", err)
	}
}
