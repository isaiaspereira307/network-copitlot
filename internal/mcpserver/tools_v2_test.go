package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/config"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/store"
)

func newTestServer(t *testing.T) (*Server, *projects.ActiveState) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load()
	cfg.WorkspacePath = t.TempDir()
	cfg.Save()
	repo := projects.NewRepo(cfg.WorkspacePath)
	active := projects.NewActiveState(repo, cfg)
	auditDir := t.TempDir()
	al, _ := audit.New(filepath.Join(auditDir, "audit.log"))
	t.Cleanup(func() { al.Close() })
	s := New(active, repo, al)
	registerV2Tools(s)
	return s, active
}

func TestCreateProjectTool(t *testing.T) {
	s, _ := newTestServer(t)
	out, err := callTool(t, s, "create_project", map[string]any{
		"name": "HackerOne-X",
		"type": "bugbounty",
	})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	if out == "" {
		t.Fatal("empty output")
	}
	repo := projects.NewRepo(getWorkspace(t))
	p, err := repo.LoadProject("HackerOne-X")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Type != projects.ProjectBugBounty {
		t.Errorf("type = %q", p.Type)
	}
}

func TestListProjectsTool(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "A", "type": "pentest"})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	_, err = callTool(t, s, "create_project", map[string]any{"name": "B", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	out, err := callTool(t, s, "list_projects", map[string]any{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestSetActiveProjectTool(t *testing.T) {
	s, active := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P1", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := callTool(t, s, "set_active_project", map[string]any{"name": "P1"})
	if err != nil {
		t.Fatalf("set_active: %v", err)
	}
	if out == "" {
		t.Fatal("empty")
	}
	if p, _ := active.Project(); p == nil {
		t.Fatal("active project not set")
	}
}

func TestSetActiveProject_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	out, err := callTool(t, s, "set_active_project", map[string]any{"name": "NOPE"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if out != "" {
		t.Errorf("expected empty out on error, got %q", out)
	}
}

func TestAddTargetTool_RequiresConfirmation(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	_, err = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	if err != nil {
		t.Fatalf("set_active_project: %v", err)
	}
	// sem confirmed: true -> erro
	_, err = s.tools["add_target"](context.Background(), map[string]any{"host": "x.com"})
	if err == nil {
		t.Fatal("expected error when confirmation missing")
	}
}

func TestAddTargetTool_WithConfirmation(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	_, err = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	if err != nil {
		t.Fatalf("set_active_project: %v", err)
	}
	out, err := callTool(t, s, "add_target", map[string]any{
		"host":      "api.empresa.com",
		"confirmed": true,
	})
	if err != nil {
		t.Fatalf("add_target: %v", err)
	}
	if out == "" {
		t.Fatal("empty")
	}
	// verifica persistencia
	repo := projects.NewRepo(getWorkspace(t))
	tgt, err := repo.LoadTarget("P", "api.empresa.com")
	if err != nil {
		t.Fatalf("load target: %v", err)
	}
	if tgt.Host != "api.empresa.com" {
		t.Errorf("host = %q", tgt.Host)
	}
}

func TestAddTargetTool_NoActiveProject(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := s.tools["add_target"](context.Background(), map[string]any{"host": "x.com", "confirmed": true})
	if err == nil {
		t.Fatal("expected error: no active project")
	}
}

func TestListTargetsTool(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	_, err = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	if err != nil {
		t.Fatalf("set_active_project: %v", err)
	}
	_, err = callTool(t, s, "add_target", map[string]any{"host": "a.com", "confirmed": true})
	if err != nil {
		t.Fatalf("add_target a: %v", err)
	}
	_, err = callTool(t, s, "add_target", map[string]any{"host": "b.com", "confirmed": true})
	if err != nil {
		t.Fatalf("add_target b: %v", err)
	}
	out, err := callTool(t, s, "list_targets", map[string]any{})
	if err != nil {
		t.Fatalf("list_targets: %v", err)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestSetActiveTargetTool(t *testing.T) {
	s, active := newTestServer(t)
	_, err := callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	if err != nil {
		t.Fatalf("create_project: %v", err)
	}
	_, err = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	if err != nil {
		t.Fatalf("set_active_project: %v", err)
	}
	_, err = callTool(t, s, "add_target", map[string]any{"host": "x.com", "confirmed": true})
	if err != nil {
		t.Fatalf("add_target: %v", err)
	}
	_, err = callTool(t, s, "set_active_target", map[string]any{"host": "x.com"})
	if err != nil {
		t.Fatalf("set_active_target: %v", err)
	}
	tgt, _ := active.Target()
	if tgt == nil || tgt.Host != "x.com" {
		t.Errorf("active target not set: %+v", tgt)
	}
}

// callTool: retorna (out, err) para que testes de erro e sucesso compartilhem
// o mesmo helper. Variante do brief (que so retornava out e fazia Fatalf).
func callTool(t *testing.T, s *Server, name string, args map[string]any) (string, error) {
	t.Helper()
	fn, ok := s.tools[name]
	if !ok {
		t.Fatalf("tool %s not registered", name)
	}
	return fn(context.Background(), args)
}

func getWorkspace(t *testing.T) string {
	t.Helper()
	cfg, _ := config.Load()
	return cfg.WorkspacePath
}

func TestGetActiveContextTool_Empty(t *testing.T) {
	s, _ := newTestServer(t)
	out, _ := callTool(t, s, "get_active_context", map[string]any{})
	if out == "" {
		t.Fatal("empty")
	}
	// sem projeto ativo: retorna JSON com active_project=""
	var ctx map[string]any
	_ = json.Unmarshal([]byte(out), &ctx)
	if ctx["active_project"] != "" {
		t.Errorf("expected empty, got %+v", ctx)
	}
}

func TestGetActiveContextTool_Full(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "x.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "x.com"})
	out, err := callTool(t, s, "get_active_context", map[string]any{})
	if err != nil {
		t.Fatalf("get_active_context: %v", err)
	}
	var ctx map[string]any
	_ = json.Unmarshal([]byte(out), &ctx)
	if ctx["active_project"] != "P" {
		t.Errorf("project = %v", ctx["active_project"])
	}
	if ctx["active_target"] != "x.com" {
		t.Errorf("target = %v", ctx["active_target"])
	}
	if v, ok := ctx["request_count"].(float64); !ok {
		t.Errorf("request_count missing or wrong type: %T", ctx["request_count"])
	} else if v != 0 {
		t.Errorf("request_count = %v, want 0", v)
	}
}

func TestListRequestsTool_ReturnsOnlySummary(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if st == nil {
		t.Fatal("nil store for active target")
	}
	for i := 0; i < 3; i++ {
		_, err := st.Insert(&store.Request{
			Ts:          time.Now().UnixMilli() + int64(i),
			Method:      "GET",
			URL:         fmt.Sprintf("https://api.empresa.com/v%d/users", i),
			Status:      200,
			RespLen:     100 + i,
			ReqBody:     []byte("secret-request-body"),
			RespBody:    []byte("secret-response-body"),
			ReqHeaders:  map[string][]string{"Cookie": {"SECRET=1"}},
			RespHeaders: map[string][]string{},
		})
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	out, err := callTool(t, s, "list_requests", map[string]any{})
	if err != nil {
		t.Fatalf("list_requests: %v", err)
	}
	var res struct {
		Count    int              `json:"count"`
		Requests []map[string]any `json:"requests"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if res.Count != 3 || len(res.Requests) != 3 {
		t.Fatalf("expected 3 requests, got count=%d len=%d body=%s", res.Count, len(res.Requests), out)
	}
	for _, r := range res.Requests {
		for _, key := range []string{"id", "ts", "method", "url", "status", "resp_len"} {
			if _, ok := r[key]; !ok {
				t.Errorf("missing summary key %q in %v", key, r)
			}
		}
		for _, bad := range []string{"req_body", "resp_body", "req_headers", "resp_headers", "notes"} {
			if _, ok := r[bad]; ok {
				t.Errorf("body field %q leaked: %v", bad, r)
			}
		}
	}
}

func TestListRequestsTool_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "list_requests", map[string]any{})
	if err == nil {
		t.Fatal("expected error with no active target")
	}
}

func TestListRequestsTool_LimitClamp(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	for i := 0; i < 201; i++ {
		if _, err := st.Insert(&store.Request{Ts: time.Now().UnixMilli() + int64(i), Method: "GET", URL: fmt.Sprintf("https://api.empresa.com/r/%d", i), Status: 200}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	out, err := callTool(t, s, "list_requests", map[string]any{"limit": 9999})
	if err != nil {
		t.Fatalf("list_requests: %v", err)
	}
	var res struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Count != 200 {
		t.Errorf("expected clamp to 200, got %d", res.Count)
	}
}

func TestGetRequestDetail_TruncatesByDefault(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if st == nil {
		t.Fatal("nil store for active target")
	}
	big := make([]byte, 20000) // > 8192 default
	for i := range big {
		big[i] = 'x'
	}
	id, err := st.Insert(&store.Request{
		Ts:          time.Now().UnixMilli(),
		Method:      "POST",
		URL:         "https://api.empresa.com/upload",
		ReqHeaders:  map[string][]string{"Content-Type": {"application/json"}},
		ReqBody:     big,
		Status:      201,
		RespHeaders: map[string][]string{},
		RespBody:    big,
		RespLen:     len(big),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	out, err := callTool(t, s, "get_request_detail", map[string]any{"id": id, "include": "all"})
	if err != nil {
		t.Fatalf("get_request_detail: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	for _, key := range []string{"id", "ts", "method", "url", "req_headers", "req_body", "status", "resp_headers", "resp_body", "resp_len", "req_body_truncated", "resp_body_truncated", "req_total_len", "resp_total_len"} {
		if _, ok := res[key]; !ok {
			t.Errorf("missing key %q in %v", key, res)
		}
	}
	if res["req_body_truncated"] != true || res["resp_body_truncated"] != true {
		t.Errorf("expected truncated flags, got req=%v resp=%v", res["req_body_truncated"], res["resp_body_truncated"])
	}
	if res["req_total_len"] != float64(20000) || res["resp_total_len"] != float64(20000) {
		t.Errorf("total len = %v/%v, want 20000/20000", res["req_total_len"], res["resp_total_len"])
	}
	if len(res["req_body"].(string)) > 8192 || len(res["resp_body"].(string)) > 8192 {
		t.Errorf("body exceeded 8192: req=%d resp=%d", len(res["req_body"].(string)), len(res["resp_body"].(string)))
	}
}

func TestGetRequestDetail_InvalidID(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	if _, err := callTool(t, s, "get_request_detail", map[string]any{}); err == nil {
		t.Fatal("expected error: missing id")
	}
	if _, err := callTool(t, s, "get_request_detail", map[string]any{"id": "abc"}); err == nil {
		t.Fatal("expected error: non-numeric id")
	}
}

func TestGetRequestDetail_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "get_request_detail", map[string]any{"id": 1})
	if err == nil {
		t.Fatal("expected error with no active target")
	}
}

func TestSearchBodiesTool_ReturnsSnippetNotBody(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if st == nil {
		t.Fatal("nil store for active target")
	}
	// corpo grande (> 2*snippetWindow) para provar que so o snippet sai
	filler := strings.Repeat("x", 500)
	id, err := st.Insert(&store.Request{
		Ts:       time.Now().UnixMilli(),
		Method:   "POST",
		URL:      "https://api.empresa.com/login",
		ReqBody:  []byte(filler + "NEEDLE_MARK" + filler),
		Status:   200,
		RespBody: []byte(filler + "NEEDLE_MARK" + filler),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	out, err := callTool(t, s, "search_bodies", map[string]any{"query": "NEEDLE_MARK"})
	if err != nil {
		t.Fatalf("search_bodies: %v", err)
	}
	var res struct {
		Count   int `json:"count"`
		Matches []struct {
			ID      int64  `json:"id"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if res.Count != 1 || len(res.Matches) != 1 {
		t.Fatalf("expected 1 match, got count=%d len=%d body=%s", res.Count, len(res.Matches), out)
	}
	m := res.Matches[0]
	if m.ID != id {
		t.Errorf("id = %d, want %d", m.ID, id)
	}
	if m.URL != "https://api.empresa.com/login" {
		t.Errorf("url = %q", m.URL)
	}
	if !strings.Contains(m.Snippet, "NEEDLE_MARK") {
		t.Errorf("snippet %q does not contain needle", m.Snippet)
	}
	if len(m.Snippet) > 200 {
		t.Errorf("snippet too long: %d chars", len(m.Snippet))
	}
	// token pillar: nenhum campo com corpo completo nas entradas retornadas
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, mm := range raw["matches"].([]any) {
		mv := mm.(map[string]any)
		for _, bad := range []string{"req_body", "resp_body", "body", "req_headers", "resp_headers"} {
			if _, ok := mv[bad]; ok {
				t.Errorf("body field %q leaked: %v", bad, mv)
			}
		}
		if _, ok := mv["snippet"]; !ok {
			t.Error("match missing snippet field")
		}
	}
}

func TestSearchBodiesTool_Validation(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	if _, err := callTool(t, s, "search_bodies", map[string]any{}); err == nil {
		t.Fatal("expected error: missing query")
	}
	if _, err := callTool(t, s, "search_bodies", map[string]any{"query": "x", "scope": "nope"}); err == nil {
		t.Fatal("expected error: invalid scope")
	}
	// scope valido + limit clamp nao quebram
	if _, err := callTool(t, s, "search_bodies", map[string]any{"query": "x", "scope": "resp", "limit": 9999}); err != nil {
		t.Fatalf("valid call failed: %v", err)
	}
}

func TestSearchBodiesTool_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "search_bodies", map[string]any{"query": "x"})
	if err == nil {
		t.Fatal("expected error with no active target")
	}
}

var _ = time.Now

// TestReplayTool_RejectsOutOfScope (11.1): alvo ativo com escopo que EXCLUI o
// host do request capturado -> erro "fora do escopo", nada persistido, e a
// mensagem cita o host bloqueado.
func TestReplayTool_RejectsOutOfScope(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	// in_scope so casa *.corp.example.com -> api.empresa.com fica FORA
	_, _ = callTool(t, s, "add_target", map[string]any{
		"host":      "api.empresa.com",
		"confirmed": true,
		"in_scope":  []any{"*.corp.example.com"},
	})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	id, err := st.Insert(&store.Request{
		Ts: time.Now().UnixMilli(), Method: "GET",
		URL: "https://api.empresa.com/users", ReqHeaders: map[string][]string{},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, err = callTool(t, s, "replay_request", map[string]any{"id": id})
	if err == nil {
		t.Fatal("expected out-of-scope error")
	}
	if !strings.Contains(err.Error(), "fora do escopo") {
		t.Errorf("err = %v, want mensagem 'fora do escopo'", err)
	}
	if !strings.Contains(err.Error(), "api.empresa.com") {
		t.Errorf("err = %v, deve citar o host bloqueado", err)
	}
	// tool call reabre o store (fecha o anterior): reabre p/ inspecionar
	st, err = s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("reopen store: %v", err)
	}
	if n, _ := st.Count(); n != 1 {
		t.Errorf("count = %d, want 1 (replay fora de escopo nao persistido)", n)
	}
}

// TestReplayTool_HappyPathInScope: escopo do alvo INCLUI o host do upstream
// (httptest em 127.0.0.1); replay executa, persiste novo request e retorna
// new_request_id/status/resp_len (sem corpos).
func TestReplayTool_HappyPathInScope(t *testing.T) {
	s, _ := newTestServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)

	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{
		"host":      "api.empresa.com",
		"confirmed": true,
		"in_scope":  []any{upURL.Hostname()},
	})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	id, err := st.Insert(&store.Request{
		Ts: time.Now().UnixMilli(), Method: "GET",
		URL: upstream.URL + "/orig", ReqHeaders: map[string][]string{},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	out, err := callTool(t, s, "replay_request", map[string]any{"id": id})
	if err != nil {
		t.Fatalf("replay_request: %v", err)
	}
	var res struct {
		NewRequestID int64 `json:"new_request_id"`
		Status       int   `json:"status"`
		RespLen      int   `json:"resp_len"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if res.NewRequestID == 0 || res.NewRequestID == id {
		t.Errorf("new_request_id = %d, want novo id != %d", res.NewRequestID, id)
	}
	if res.Status != http.StatusOK || res.RespLen != 4 {
		t.Errorf("res = %+v, want status 200 resp_len 4", res)
	}
	if strings.Contains(out, "pong") {
		t.Errorf("resposta nao deve conter o corpo: %s", out)
	}
	// tool call reabre o store (fecha o anterior): reabre p/ ver o replay
	st, err = s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("reopen store: %v", err)
	}
	got, err := st.Get(res.NewRequestID)
	if err != nil || got.Status != http.StatusOK || string(got.RespBody) != "pong" {
		t.Errorf("persistido = %+v (%v), want 200 'pong'", got, err)
	}
}

// TestReplayTool_OverridesViaTool: overrides url/method/headers/body chegam ao
// upstream (host ainda in-scope).
func TestReplayTool_OverridesViaTool(t *testing.T) {
	s, _ := newTestServer(t)
	var gotMethod, gotXNew, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotXNew = r.Header.Get("X-New")
		b := make([]byte, r.ContentLength)
		if len(b) > 0 {
			r.Body.Read(b)
			gotBody = string(b)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)

	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{
		"host":      "api.empresa.com",
		"confirmed": true,
		"in_scope":  []any{upURL.Hostname()},
	})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	id, err := st.Insert(&store.Request{
		Ts: time.Now().UnixMilli(), Method: "GET",
		URL: upstream.URL, ReqHeaders: map[string][]string{"X-New": {"old"}},
		ReqBody: []byte("corpo-original"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	out, err := callTool(t, s, "replay_request", map[string]any{
		"id":              id,
		"method":          "POST",
		"headers":         map[string]any{"X-New": "novo"},
		"body":            "corpo-novo",
		"follow_redirects": false,
	})
	if err != nil {
		t.Fatalf("replay_request: %v", err)
	}
	var res struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if res.Status != http.StatusNoContent {
		t.Errorf("status = %d, want 204", res.Status)
	}
	if gotMethod != "POST" || gotXNew != "novo" || gotBody != "corpo-novo" {
		t.Errorf("upstream recebeu method=%q x-new=%q body=%q, want POST/novo/corpo-novo", gotMethod, gotXNew, gotBody)
	}
}

// TestReplayTool_Validation: id ausente/invalido e sem alvo ativo dão erro
// claro, sem panic.
func TestReplayTool_Validation(t *testing.T) {
	s, _ := newTestServer(t)
	if _, err := callTool(t, s, "replay_request", map[string]any{"id": 1}); err == nil {
		t.Fatal("expected error with no active target")
	}

	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	if _, err := callTool(t, s, "replay_request", map[string]any{}); err == nil {
		t.Fatal("expected error: missing id")
	}
	if _, err := callTool(t, s, "replay_request", map[string]any{"id": "abc"}); err == nil {
		t.Fatal("expected error: non-numeric id")
	}
}
