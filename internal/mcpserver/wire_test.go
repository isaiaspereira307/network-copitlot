package mcpserver

import "testing"

func TestRegisterTools_NoPanic(t *testing.T) {
	s := New(nil, nil, nil)
	s.RegisterTools()
	if got := len(s.mcp.ListTools()); got != 46 {
		t.Fatalf("expected 46 tools registered, got %d", got)
	}
}
