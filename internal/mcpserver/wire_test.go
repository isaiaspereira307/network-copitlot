package mcpserver

import "testing"

func TestRegisterTools_NoPanic(t *testing.T) {
	s := New(nil, nil, nil)
	s.RegisterTools()
	if got := len(s.mcp.ListTools()); got != 47 { // 46 + export_curl (v5.1 task 1)
		t.Fatalf("expected 47 tools registered, got %d", got)
	}
}
