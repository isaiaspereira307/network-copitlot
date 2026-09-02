package mcpserver

import "testing"

func TestRegisterTools_NoPanic(t *testing.T) {
	s := New(nil, nil, nil)
	s.RegisterTools()
	if got := len(s.mcp.ListTools()); got != 48 { // 46 + export_curl + export_har (v5.1 tasks 1-2)
		t.Fatalf("expected 48 tools registered, got %d", got)
	}
}
