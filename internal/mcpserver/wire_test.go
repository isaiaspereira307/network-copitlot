package mcpserver

import "testing"

func TestRegisterTools_NoPanic(t *testing.T) {
	s := New(nil, nil, nil)
	s.RegisterTools()
	if got := len(s.mcp.ListTools()); got != 10 {
		t.Fatalf("expected 10 tools registered, got %d", got)
	}
}
