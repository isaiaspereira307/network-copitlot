package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

func TestListEndpointsTool_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "list_endpoints", map[string]any{})
	if err == nil {
		t.Fatal("expected error with no active target")
	}
	if err.Error() != "nenhum alvo ativo: selecione um alvo com set_active_target" {
		t.Errorf("err = %q, want mensagem exata de alvo ativo", err.Error())
	}
}

func TestDiffRequestsTool_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "diff_requests", map[string]any{"id_a": 1, "id_b": 2})
	if err == nil {
		t.Fatal("expected error with no active target")
	}
	if err.Error() != "nenhum alvo ativo: selecione um alvo com set_active_target" {
		t.Errorf("err = %q, want mensagem exata de alvo ativo", err.Error())
	}
}

func TestDiffRequestsTool_UnifiedDiff(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	idA, err := st.Insert(&store.Request{
		Ts: 1, Method: "GET", URL: "https://api.empresa.com/users",
		Status: 200, RespLen: 40,
		RespBody: []byte("{\"ok\": true}\n{\"token\": \"abc\"}\n"),
	})
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	idB, err := st.Insert(&store.Request{
		Ts: 2, Method: "GET", URL: "https://api.empresa.com/users",
		Status: 200, RespLen: 40,
		RespBody: []byte("{\"ok\": true}\n{\"token\": \"def\"}\n{\"extra\": true}\n"),
	})
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}

	out, err := callTool(t, s, "diff_requests", map[string]any{"id_a": idA, "id_b": idB, "mode": "resp"})
	if err != nil {
		t.Fatalf("diff_requests: %v", err)
	}
	var res struct {
		ID_A     float64  `json:"id_a"`
		ID_B     float64  `json:"id_b"`
		Mode     string   `json:"mode"`
		ChangedA int      `json:"changed_a"`
		ChangedB int      `json:"changed_b"`
		Diff     []string `json:"diff"`
		Trunc    bool     `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if res.ID_A != float64(idA) || res.ID_B != float64(idB) || res.Mode != "resp" {
		t.Errorf("meta = %+v", res)
	}
	if res.ChangedA != 1 || res.ChangedB != 2 {
		t.Errorf("changed_a=%d changed_b=%d, want 1/2", res.ChangedA, res.ChangedB)
	}
	if res.Trunc {
		t.Errorf("truncated=true para diff pequeno")
	}
	joined := strings.Join(res.Diff, "\n")
	if !strings.Contains(joined, `-{"token": "abc"}`) || !strings.Contains(joined, `+{"token": "def"}`) {
		t.Errorf("diff sem linhas +/-:\n%s", joined)
	}
}

func TestDiffRequestsTool_BadArgs(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	if _, err := callTool(t, s, "diff_requests", map[string]any{}); err == nil {
		t.Error("id_a/id_b ausentes: esperado erro")
	}
	if _, err := callTool(t, s, "diff_requests", map[string]any{"id_a": 1, "id_b": 2, "mode": "bogus"}); err == nil {
		t.Error("mode invalido: esperado erro")
	}
}

func TestListEndpointsTool_ReturnsDedupedEndpoints(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	for i, url := range []string{
		"https://api.empresa.com/users/123",
		"https://api.empresa.com/users/456",
		"https://api.empresa.com/health",
	} {
		if _, err := st.Insert(&store.Request{Ts: time.Now().UnixMilli() + int64(i), Method: "GET", URL: url}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	out, err := callTool(t, s, "list_endpoints", map[string]any{})
	if err != nil {
		t.Fatalf("list_endpoints: %v", err)
	}
	var res struct {
		Count     int `json:"count"`
		Endpoints []struct {
			Method    string  `json:"method"`
			Path      string  `json:"path"`
			HitCount  int     `json:"hit_count"`
			SampleIDs []int64 `json:"sample_ids"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if res.Count != 2 || len(res.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got count=%d len=%d body=%s", res.Count, len(res.Endpoints), out)
	}
	byPath := map[string]struct {
		Hit  int
		IDs  []int64
	}{}
	for _, e := range res.Endpoints {
		if e.Method != "GET" {
			t.Errorf("method = %q, want GET", e.Method)
		}
		byPath[e.Path] = struct {
			Hit int
			IDs []int64
		}{e.HitCount, e.SampleIDs}
	}
	if ep := byPath["/users/{id}"]; ep.Hit != 2 || len(ep.IDs) != 2 {
		t.Errorf("/users/{id} = %+v, want hit 2, 2 sample ids", ep)
	}
	if ep := byPath["/health"]; ep.Hit != 1 || len(ep.IDs) != 1 {
		t.Errorf("/health = %+v, want hit 1, 1 sample id", ep)
	}
}

func setupTarget(t *testing.T, s *Server) {
	t.Helper()
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})
}

func TestSummarizeResponseTool_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "summarize_response", map[string]any{"id": 1})
	if err == nil {
		t.Fatal("expected error with no active target")
	}
	if err.Error() != "nenhum alvo ativo: selecione um alvo com set_active_target" {
		t.Errorf("err = %q, want mensagem exata de alvo ativo", err.Error())
	}
}

func TestSummarizeResponseTool_HtmlSummary(t *testing.T) {
	s, _ := newTestServer(t)
	setupTarget(t, s)
	htmlBody := `<html><body>
  <form action="/login" method="POST"><input name="user"><input name="pass"></form>
  <a href="/profile">Profile</a>
  <script src="/static/app.js"></script>
  </body></html>`
	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	id, err := st.Insert(&store.Request{
		Ts: 1, Method: "GET", URL: "https://api.empresa.com/login",
		Status: 200, RespLen: len(htmlBody),
		RespHeaders: map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
		RespBody:    []byte(htmlBody),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	out, err := callTool(t, s, "summarize_response", map[string]any{"id": id})
	if err != nil {
		t.Fatalf("summarize_response: %v", err)
	}
	var res struct {
		Kind        string         `json:"kind"`
		ContentType string         `json:"content_type"`
		Truncated   bool           `json:"truncated"`
		TotalLen    int            `json:"total_len"`
		Detail      map[string]any `json:"detail"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if res.Kind != "html" || res.ContentType != "text/html; charset=utf-8" {
		t.Errorf("kind=%q ct=%q", res.Kind, res.ContentType)
	}
	if res.Truncated || res.TotalLen != len(htmlBody) {
		t.Errorf("truncated=%v total=%d, want false/%d", res.Truncated, res.TotalLen, len(htmlBody))
	}
	forms, ok := res.Detail["forms"].([]any)
	if !ok || len(forms) != 1 {
		t.Fatalf("detail.forms = %#v, want 1 form", res.Detail["forms"])
	}
	form0 := forms[0].(map[string]any)
	if form0["action"] != "/login" || form0["method"] != "POST" {
		t.Errorf("form[0] = %+v, want action=/login method=POST", form0)
	}
	if !strings.Contains(out, "/static/app.js") {
		t.Errorf("out sem script externo:\n%s", out)
	}
}

func TestSummarizeResponseTool_BadOrMissingID(t *testing.T) {
	s, _ := newTestServer(t)
	setupTarget(t, s)
	if _, err := callTool(t, s, "summarize_response", map[string]any{}); err == nil {
		t.Error("id ausente: esperado erro")
	}
	if _, err := callTool(t, s, "summarize_response", map[string]any{"id": 9999}); err == nil {
		t.Error("id inexistente: esperado erro")
	}
}
