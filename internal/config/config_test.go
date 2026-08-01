package config

import (
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.WorkspacePath == "" {
		t.Fatal("WorkspacePath must be set in default")
	}
	if !filepath.IsAbs(c.WorkspacePath) {
		t.Fatalf("WorkspacePath must be absolute, got %q", c.WorkspacePath)
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // Path() usa $HOME

	c := Default()
	c.ActiveProject = "HackerOne-EMPRESA"
	c.ActiveTarget = "api.empresa.com"
	if err := c.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ActiveProject != "HackerOne-EMPRESA" {
		t.Errorf("active_project = %q, want HackerOne-EMPRESA", loaded.ActiveProject)
	}
	if loaded.ActiveTarget != "api.empresa.com" {
		t.Errorf("active_target = %q, want api.empresa.com", loaded.ActiveTarget)
	}
}

func TestLoad_DefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	c, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.ActiveProject != "" {
		t.Errorf("expected empty active_project, got %q", c.ActiveProject)
	}
}
