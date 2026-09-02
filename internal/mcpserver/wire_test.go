package mcpserver

import "testing"

func TestRegisterTools_NoPanic(t *testing.T) {
	s := New(nil, nil, nil)
	s.RegisterTools()
	if got := len(s.mcp.ListTools()); got != 49 { // 46 + export_curl + export_har + jwt_decode (v5.1 tasks 1-3)
		t.Fatalf("expected 49 tools registered, got %d", got)
	}
}
