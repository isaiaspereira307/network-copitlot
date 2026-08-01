package projects

import (
	"testing"
	"time"

	"github.com/isaias/network-copitlot/internal/config"
)

func newTestActive(t *testing.T) (*ActiveState, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.WorkspacePath = t.TempDir()
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return NewActiveState(NewRepo(cfg.WorkspacePath), cfg), cfg
}

func TestActiveState_SetProject(t *testing.T) {
	a, _ := newTestActive(t)
	now := time.Now()
	if err := a.repo.CreateProject(&Project{Name: "P1", Type: ProjectBugBounty, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProject("P1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if a.config.ActiveProject != "P1" {
		t.Errorf("active = %q, want P1", a.config.ActiveProject)
	}
}

func TestActiveState_SetProject_NotFound(t *testing.T) {
	a, _ := newTestActive(t)
	if err := a.SetProject("NOPE"); err == nil {
		t.Fatal("expected error")
	}
}

func TestActiveState_SetTarget_NoProject(t *testing.T) {
	a, _ := newTestActive(t)
	if err := a.SetTarget("x.com"); err == nil {
		t.Fatal("expected error when no project active")
	}
}

func TestActiveState_SetTarget_Roundtrip(t *testing.T) {
	a, _ := newTestActive(t)
	now := time.Now()
	if err := a.repo.CreateProject(&Project{Name: "P", Type: ProjectBugBounty, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProject("P"); err != nil {
		t.Fatal(err)
	}
	if err := a.repo.AddTarget("P", &Target{Host: "x.com", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetTarget("x.com"); err != nil {
		t.Fatal(err)
	}
	tgt, err := a.Target()
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Host != "x.com" {
		t.Errorf("host = %q", tgt.Host)
	}
}

func TestActiveState_Project_Empty(t *testing.T) {
	a, _ := newTestActive(t)
	p, err := a.Project()
	if err != nil {
		t.Fatal(err)
	}
	if p != nil {
		t.Errorf("expected nil, got %+v", p)
	}
}
