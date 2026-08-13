package mcpserver

import "testing"

func TestRegisterTools_NoPanic(t *testing.T) {
	s := New(nil, nil, nil)
	s.RegisterTools()
	if got := len(s.mcp.ListTools()); got != 11 {
		t.Fatalf("expected 11 tools registered, got %d", got)
	}
}
