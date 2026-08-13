package e2e

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/config"
	"github.com/isaias/network-copitlot/internal/mcpserver"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/proxy"
	"github.com/isaias/network-copitlot/internal/store"
)

// TestE2E_P0_Tools cobre o fluxo completo Sprint 1 (P0): browse pelo proxy
// MITM -> list_requests -> search_bodies -> get_request_detail -> replay_request.
// Wire real (como main): MCP server e proxy compartilham o requests.db do alvo
// ativo; escopo do alvo vem do meta.yaml persistido por add_target.
func TestE2E_P0_Tools(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load()
	cfg.WorkspacePath = filepath.Join(dir, "ws")
	cfg.Save()
	repo := projects.NewRepo(cfg.WorkspacePath)
	active := projects.NewActiveState(repo, cfg)
	al, _ := audit.New(filepath.Join(dir, "audit.log"))
	t.Cleanup(func() { al.Close() })
	srv := mcpserver.New(active, repo, al)

	// setup via tools (caminho real do MCP, nao atalho de repo)
	call(t, srv, "create_project", map[string]any{"name": "P1", "type": "bugbounty"})
	call(t, srv, "set_active_project", map[string]any{"name": "P1"})
	call(t, srv, "add_target", map[string]any{
		"host": "127.0.0.1", "confirmed": true, "in_scope": []any{"127.0.0.1"},
	})
	call(t, srv, "set_active_target", map[string]any{"host": "127.0.0.1"})

	proj, _ := active.Project()
	tgt, _ := active.Target()
	targetDir := tgt.Dir(proj.Dir(repo.WorkspacePath()))

	// proxy com store no MESMO requests.db que o MCP server abre
	st, err := store.OpenSQLite(filepath.Join(targetDir, "requests.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	caDir := filepath.Join(dir, "ca")
	if _, _, err := proxy.EnsureCA(caDir); err != nil {
		t.Fatal(err)
	}
	p := proxy.NewProxy(st, caDir)
	p.SetTarget(tgt)
	if err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	proxyURL, _ := url.Parse("http://" + p.Addr())

	// upstream HTTPS (127.0.0.1:porta — in-scope do alvo)
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(up.Close)

	// browse: 3 requests distintos via proxy (in-scope = body capturado)
	needle := "NEEDLE_XYZ"
	browse(t, proxyURL, up.URL+"/api/users?id=42", "GET", nil)
	browse(t, proxyURL, up.URL+"/api/login", "POST", []byte(`{"user":"admin","token":"`+needle+`"}`))
	browse(t, proxyURL, up.URL+"/health", "GET", nil)

	// list: 3 resumos, ordem de recencia (id DESC), so keys de resumo
	out := call(t, srv, "list_requests", map[string]any{})
	var list listResp
	mustUnmarshal(t, out, &list)
	if list.Count != 3 {
		t.Fatalf("list: count=%d, esperado 3", list.Count)
	}
	if len(list.Requests) != 3 {
		t.Fatalf("list: %d resumos, esperado 3", len(list.Requests))
	}
	postID := int64(-1)
	for i, sm := range list.Requests {
		id := idOf(t, sm["id"])
		if i > 0 {
			prev := idOf(t, list.Requests[i-1]["id"])
			if id >= prev {
				t.Fatalf("list: ordem de recencia quebrada: %d depois de %d", id, prev)
			}
		}
		for _, k := range []string{"req_body", "resp_body", "req_headers", "resp_headers"} {
			if _, has := sm[k]; has {
				t.Fatalf("list: resumo vazou body/header key %q", k)
			}
		}
		if u, _ := sm["url"].(string); strings.Contains(u, "/api/login") {
			postID = id
		}
	}
	if postID < 0 {
		t.Fatal("list: POST /api/login nao encontrado")
	}

	// search: agulha no body do POST -> um hit, id == postID, snippet contem
	out = call(t, srv, "search_bodies", map[string]any{"query": needle})
	var sh searchResp
	mustUnmarshal(t, out, &sh)
	if sh.Count != 1 {
		t.Fatalf("search: count=%d, esperado 1", sh.Count)
	}
	if got := idOf(t, sh.Matches[0]["id"]); got != postID {
		t.Fatalf("search: id=%d, esperado %d", got, postID)
	}
	snip, _ := sh.Matches[0]["snippet"].(string)
	if !strings.Contains(snip, needle) {
		t.Fatalf("search: snippet %q nao contem %q", snip, needle)
	}

	// detail: headers/body/status/flags de truncamento
	out = call(t, srv, "get_request_detail", map[string]any{"id": postID, "include": "all"})
	var det map[string]any
	mustUnmarshal(t, out, &det)
	if m, _ := det["method"].(string); m != "POST" {
		t.Fatalf("detail: method=%q", m)
	}
	if u, _ := det["url"].(string); !strings.Contains(u, "/api/login") {
		t.Fatalf("detail: url=%q", u)
	}
	if rb, _ := det["req_body"].(string); !strings.Contains(rb, needle) {
		t.Fatalf("detail: req_body sem agulha")
	}
	if status := int(det["status"].(float64)); status != 200 {
		t.Fatalf("detail: status=%d", status)
	}
	if rh, ok := det["req_headers"].(map[string]any); !ok || len(rh) == 0 {
		t.Fatal("detail: req_headers ausente/vazio")
	}
	if rl, ok := det["resp_len"].(float64); !ok || rl <= 0 {
		t.Fatalf("detail: resp_len=%v", det["resp_len"])
	}
	if tr, _ := det["req_body_truncated"].(bool); tr {
		t.Fatal("detail: req_body_truncated=true em body pequeno")
	}
	if tr, _ := det["resp_body_truncated"].(bool); tr {
		t.Fatal("detail: resp_body_truncated=true em body pequeno")
	}

	// replay: novo request persistido (4 agora), scope guard passa p/ 127.0.0.1
	out = call(t, srv, "replay_request", map[string]any{"id": postID})
	var rep map[string]any
	mustUnmarshal(t, out, &rep)
	newID := int64(rep["new_request_id"].(float64))
	if newID <= 0 {
		t.Fatalf("replay: new_request_id=%d", newID)
	}
	if status := int(rep["status"].(float64)); status != 200 {
		t.Fatalf("replay: status=%d", status)
	}

	out = call(t, srv, "list_requests", map[string]any{})
	mustUnmarshal(t, out, &list)
	if list.Count != 4 {
		t.Fatalf("replay: count=%d, esperado 4 (3 + replay)", list.Count)
	}
	if idOf(t, list.Requests[0]["id"]) != newID {
		t.Fatalf("replay: request mais recente deveria ser o novo (%d), got %v", newID, list.Requests[0]["id"])
	}

	// replay persistido com detalhe real: metodo/url herdados do original
	out = call(t, srv, "get_request_detail", map[string]any{"id": newID, "include": "all"})
	mustUnmarshal(t, out, &det)
	if m, _ := det["method"].(string); m != "POST" {
		t.Fatalf("replay detail: method=%q", m)
	}
	if u, _ := det["url"].(string); !strings.Contains(u, "/api/login") {
		t.Fatalf("replay detail: url=%q", u)
	}
}

// call: helper identico ao padrao callTool do internal/mcpserver, via a via
// publica CallTool (o campo tools e interno).
func call(t *testing.T, s *mcpserver.Server, name string, args map[string]any) string {
	t.Helper()
	out, err := s.CallTool(name, args)
	if err != nil {
		t.Fatalf("tool %s: %v", name, err)
	}
	return out
}

func browse(t *testing.T, proxyURL *url.URL, rawurl, method string, body []byte) {
	t.Helper()
	tr := &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: tr}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, rawurl, rdr)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s via proxy: %v", method, rawurl, err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("%s %s: status=%d", method, rawurl, resp.StatusCode)
	}
	if !bytes.Contains(got, []byte(`"status":"ok"`)) {
		t.Fatalf("%s %s: body=%q", method, rawurl, got)
	}
}

type listResp struct {
	Count    int              `json:"count"`
	Requests []map[string]any `json:"requests"`
}

type searchResp struct {
	Count   int              `json:"count"`
	Matches []map[string]any `json:"matches"`
}

func idOf(t *testing.T, v any) int64 {
	t.Helper()
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	}
	t.Fatalf("id com tipo inesperado: %T (%v)", v, v)
	return 0
}

func mustUnmarshal(t *testing.T, raw string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
}
