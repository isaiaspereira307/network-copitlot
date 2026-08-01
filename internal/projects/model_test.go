package projects

import (
	"strings"
	"testing"
	"time"
)

func TestProjectType_Valid(t *testing.T) {
	cases := []struct {
		in   ProjectType
		want bool
	}{
		{ProjectBugBounty, true},
		{ProjectPentest, true},
		{ProjectType(""), false},
		{ProjectType("hack"), false},
	}
	for _, c := range cases {
		if got := c.in.Valid(); got != c.want {
			t.Errorf("Valid(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestProject_Validate(t *testing.T) {
	now := time.Now()
	good := &Project{Name: "HackerOne-EMPRESA", Type: ProjectBugBounty, CreatedAt: now}
	if err := good.Validate(); err != nil {
		t.Errorf("good project rejected: %v", err)
	}

	bad := []struct {
		name string
		p    *Project
	}{
		{"empty name", &Project{Name: "", Type: ProjectBugBounty, CreatedAt: now}},
		{"bad name chars", &Project{Name: "../escape", Type: ProjectBugBounty, CreatedAt: now}},
		{"bad type", &Project{Name: "X", Type: "x", CreatedAt: now}},
	}
	for _, b := range bad {
		if err := b.p.Validate(); err == nil {
			t.Errorf("%s: expected error, got nil", b.name)
		}
	}
}

func TestProject_Dir(t *testing.T) {
	p := &Project{Name: "HackerOne-EMPRESA"}
	got := p.Dir("/ws")
	want := "/ws/HackerOne-EMPRESA"
	if got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestTarget_Validate(t *testing.T) {
	now := time.Now()
	good := &Target{Host: "api.empresa.com", CreatedAt: now}
	if err := good.Validate(); err != nil {
		t.Errorf("good target rejected: %v", err)
	}

	bad := []struct {
		name string
		t    *Target
	}{
		{"empty host", &Target{Host: "", CreatedAt: now}},
		{"path traversal", &Target{Host: "../etc", CreatedAt: now}},
		{"slash in host", &Target{Host: "a/b", CreatedAt: now}},
	}
	for _, b := range bad {
		if err := b.t.Validate(); err == nil {
			t.Errorf("%s: expected error, got nil", b.name)
		}
	}
}

func TestTarget_Dir(t *testing.T) {
	tgt := &Target{Host: "api.empresa.com"}
	got := tgt.Dir("/ws/HackerOne-EMPRESA")
	want := "/ws/HackerOne-EMPRESA/targets/api.empresa.com"
	if got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if strings.Contains(got, "..") {
		t.Error("Dir must not contain ..")
	}
}
