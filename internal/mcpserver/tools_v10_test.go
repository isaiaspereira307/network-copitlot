package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// setupExportTarget cria projeto/alvo ativos e devolve o store aberto.
func setupExportTarget(t *testing.T, s *Server) store.Store {
	t.Helper()
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.alvo.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.alvo.com"})
	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

func TestToolExportCurl(t *testing.T) {
	s, _ := newTestServer(t)
	st := setupExportTarget(t, s)
	id, err := st.Insert(&store.Request{
		Method: "POST", URL: "https://api.alvo.com/v1/login",
		ReqHeaders: map[string][]string{"Authorization": {"Bearer tok123"}, "Content-Type": {"application/json"}},
		ReqBody:   []byte(`{"user":"a","pass":"b"}`), Status: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.toolExportCurl(context.Background(), map[string]any{"id": float64(id)})
	if err != nil {
		t.Fatalf("toolExportCurl: %v", err)
	}
	for _, want := range []string{"curl", "-X POST", "https://api.alvo.com/v1/login", "-H 'Authorization: Bearer tok123'", "--data"} {
		if !strings.Contains(out, want) {
			t.Errorf("curl output missing %q:\n%s", want, out)
		}
	}
}

func TestToolExportCurl_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	setupExportTarget(t, s)
	if _, err := s.toolExportCurl(context.Background(), map[string]any{"id": float64(9999)}); err == nil {
		t.Error("esperava erro para id inexistente")
	}
}

func TestToolExportCurl_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	if _, err := s.toolExportCurl(context.Background(), map[string]any{"id": float64(1)}); err == nil {
		t.Error("esperava erro sem alvo ativo")
	}
}

func TestToolExportCurl_HeredocLargeBody(t *testing.T) {
	s, _ := newTestServer(t)
	st := setupExportTarget(t, s)
	body := strings.Repeat(`{"padrao":"valor-grande"}`, 11) // 22*11 = 242 bytes > 200
	if len(body) <= 200 {
		t.Fatalf("corpo de teste deve ter >200 bytes, tem %d", len(body))
	}
	id, err := st.Insert(&store.Request{
		Method: "POST", URL: "https://api.alvo.com/v1/feedback",
		ReqHeaders: map[string][]string{"Content-Type": {"application/json"}},
		ReqBody:   []byte(body), Status: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.toolExportCurl(context.Background(), map[string]any{"id": float64(id)})
	if err != nil {
		t.Fatalf("toolExportCurl: %v", err)
	}
	if !strings.Contains(out, "--data-binary @- <<'EOF'") {
		t.Errorf("esperava forma heredoc, saida:\n%s", out)
	}
	if !strings.HasSuffix(out, "EOF") {
		t.Errorf("esperava saida terminando em EOF, termina em %q", out[max(0, len(out)-20):])
	}
	if !strings.Contains(out, body) {
		t.Error("corpo grande ausente na saida")
	}
}
