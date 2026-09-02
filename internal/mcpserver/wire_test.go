package mcpserver

import "testing"

func TestRegisterTools_NoPanic(t *testing.T) {
	s := New(nil, nil, nil)
	s.RegisterTools()
	if got := len(s.mcp.ListTools()); got != 50 { // 46 + export_curl + export_har + jwt_decode + jwt_resign (v5.1 tasks 1-4)
		t.Fatalf("expected 50 tools registered, got %d", got)
	}
}
