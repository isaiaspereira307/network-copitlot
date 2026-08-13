package proxy

import (
	"net/url"
	"testing"

	"github.com/isaias/network-copitlot/internal/projects"
)

func TestScope_InScopePattern(t *testing.T) {
	s := New(&projects.Target{
		Host:            "api.empresa.com",
		InScopePatterns: []string{"*.empresa.com", "empresa.com"},
	})
	cases := []struct {
		url  string
		want bool
	}{
		{"https://api.empresa.com/v1/users", true},
		{"https://empresa.com/", true},
		{"https://app.empresa.com/", true},
		{"https://evil.com/", false},
	}
	for _, tc := range cases {
		u, _ := url.Parse(tc.url)
		if got := s.Matches(u); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.url, got, tc.want)
		}
	}
}

func TestScope_OutOfScopeOverrides(t *testing.T) {
	s := New(&projects.Target{
		Host:               "api.empresa.com",
		InScopePatterns:    []string{"*.empresa.com"},
		OutOfScopePatterns: []string{"*.admin.empresa.com"},
	})
	cases := []struct {
		url  string
		want bool
	}{
		{"https://api.empresa.com/", true},
		{"https://admin.empresa.com/", false},
		{"https://root.admin.empresa.com/", false},
	}
	for _, tc := range cases {
		u, _ := url.Parse(tc.url)
		if got := s.Matches(u); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.url, got, tc.want)
		}
	}
}

func TestScope_EmptyPatterns_AllowAll(t *testing.T) {
	// sem patterns: permite tudo (default confiante; ativo so via add_target).
	s := New(&projects.Target{Host: "x.com"})
	u, _ := url.Parse("https://anything.com/")
	if !s.Matches(u) {
		t.Error("empty scope should allow all")
	}
}

func TestScope_NoTarget_RejectsAll(t *testing.T) {
	// sem target: nao ha onde gravar; rejeita (evita captura fora de escopo).
	s := New(nil)
	u, _ := url.Parse("https://x.com/")
	if s.Matches(u) {
		t.Error("nil target should reject all")
	}
}
