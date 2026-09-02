package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

func TestFuzzRequestTool_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "fuzz_request", map[string]any{"id": 1, "point": "body", "payloads": []any{"x"}})
	if err == nil {
		t.Fatal("expected error with no active target")
	}
	if err.Error() != "nenhum alvo ativo: selecione um alvo com set_active_target" {
		t.Errorf("err = %q, want mensagem exata de alvo ativo", err.Error())
	}
}

// setupFuzzTarget cria projeto+alvo com o host do upstream in-scope e devolve o store.
func setupFuzzTarget(t *testing.T, s *Server, inScopeHost string) store.Store {
	t.Helper()
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{
		"host": "api.empresa.com", "confirmed": true, "in_scope": []any{inScopeHost},
	})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})
	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

type fuzzOut struct {
	ID        float64 `json:"id"`
	Point     string  `json:"point"`
	Count     int     `json:"count"`
	Anomalies int     `json:"anomalies"`
	Baseline  struct {
		Status  int `json:"status"`
		RespLen int `json:"resp_len"`
	} `json:"baseline"`
	Results []struct {
		Payload   string `json:"payload"`
		Status    int    `json:"status"`
		RespLen   int    `json:"resp_len"`
		Reflected bool   `json:"reflected"`
		NewID     int64  `json:"new_id"`
		Anomaly   bool   `json:"anomaly"`
		Err       string `json:"err"`
	} `json:"results"`
	Truncated bool `json:"truncated"`
}

// Upstream ecoa o parametro q no corpo -> reflexao detectada. Baseline (sem q)
// devolve corpo curto; payloads refletidos viram anomalias.
func TestFuzzRequestTool_QueryReflection(t *testing.T) {
	s, _ := newTestServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("q=" + r.URL.Query().Get("q")))
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)

	st := setupFuzzTarget(t, s, upURL.Hostname())
	// baseline: resposta "q=" (len 2)
	id, err := st.Insert(&store.Request{
		Ts: time.Now().UnixMilli(), Method: "GET",
		URL: upstream.URL + "/search", ReqHeaders: map[string][]string{},
		Status: 200, RespLen: 2, RespBody: []byte("q="),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	out, err := callTool(t, s, "fuzz_request", map[string]any{
		"id": id, "point": "query:q",
		"payloads": []any{"<script>alert(1)</script>", "harmless"},
	})
	if err != nil {
		t.Fatalf("fuzz_request: %v", err)
	}
	var res fuzzOut
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if res.Count != 2 {
		t.Fatalf("count = %d, want 2; body=%s", res.Count, out)
	}
	// ambos refletem (upstream ecoa q) -> ambos anomalias
	if res.Anomalies != 2 {
		t.Errorf("anomalies = %d, want 2", res.Anomalies)
	}
	var xss *struct {
		Payload   string `json:"payload"`
		Status    int    `json:"status"`
		RespLen   int    `json:"resp_len"`
		Reflected bool   `json:"reflected"`
		NewID     int64  `json:"new_id"`
		Anomaly   bool   `json:"anomaly"`
		Err       string `json:"err"`
	}
	for i := range res.Results {
		if strings.Contains(res.Results[i].Payload, "<script>") {
			xss = &res.Results[i]
		}
	}
	if xss == nil {
		t.Fatalf("payload xss ausente nos resultados: %s", out)
	}
	if !xss.Reflected {
		t.Errorf("xss.Reflected = false, want true (upstream ecoa q)")
	}
	if xss.NewID == 0 || xss.NewID == id {
		t.Errorf("xss.NewID = %d, want novo id != %d", xss.NewID, id)
	}
}

// payload_set builtin + point=marker; upstream sempre 200 sem eco -> sem
// anomalia, mas roda todos os payloads do set sqli.
func TestFuzzRequestTool_MarkerPayloadSet(t *testing.T) {
	s, _ := newTestServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("static"))
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)

	st := setupFuzzTarget(t, s, upURL.Hostname())
	id, err := st.Insert(&store.Request{
		Ts: time.Now().UnixMilli(), Method: "GET",
		URL: upstream.URL + "/item?id=FUZZ", ReqHeaders: map[string][]string{},
		Status: 200, RespLen: 6, RespBody: []byte("static"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// query point URL-encoda os payloads (espacos/aspas) -> URL valida ->
	// upstream sempre 200 "static": exercita o payload_set sem anomalia.
	out, err := callTool(t, s, "fuzz_request", map[string]any{
		"id": id, "point": "query:id", "payload_set": "sqli",
	})
	if err != nil {
		t.Fatalf("fuzz_request: %v", err)
	}
	var res fuzzOut
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Count != len(builtinPayloadSets["sqli"]) {
		t.Errorf("count = %d, want %d (sqli set)", res.Count, len(builtinPayloadSets["sqli"]))
	}
	if res.Anomalies != 0 {
		t.Errorf("anomalies = %d, want 0 (resposta estatica)", res.Anomalies)
	}
}

func TestFuzzRequestTool_MarkerAbsent(t *testing.T) {
	s, _ := newTestServer(t)
	st := setupFuzzTarget(t, s, "api.empresa.com")
	id, err := st.Insert(&store.Request{
		Ts: time.Now().UnixMilli(), Method: "GET",
		URL: "https://api.empresa.com/x", ReqHeaders: map[string][]string{},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	_, err = callTool(t, s, "fuzz_request", map[string]any{
		"id": id, "point": "marker", "payloads": []any{"a"},
	})
	if err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("err = %v, want erro citando marker ausente", err)
	}
}

func TestFuzzRequestTool_RejectsOutOfScope(t *testing.T) {
	s, _ := newTestServer(t)
	// in_scope so casa *.corp.example.com -> host do request fica fora
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{
		"host": "api.empresa.com", "confirmed": true, "in_scope": []any{"*.corp.example.com"},
	})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})
	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	id, err := st.Insert(&store.Request{
		Ts: time.Now().UnixMilli(), Method: "GET",
		URL: "https://api.empresa.com/x?q=1", ReqHeaders: map[string][]string{},
		Status: 200, RespLen: 10,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	out, err := callTool(t, s, "fuzz_request", map[string]any{
		"id": id, "point": "query:q", "payloads": []any{"a", "b"},
	})
	if err != nil {
		t.Fatalf("fuzz_request nao deve falhar globalmente por payload fora de escopo: %v", err)
	}
	var res fuzzOut
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, r := range res.Results {
		if r.Err == "" || !strings.Contains(r.Err, "escopo") {
			t.Errorf("payload %q err = %q, want erro de escopo", r.Payload, r.Err)
		}
	}
}

func TestMatchReplaceTools_RoundTrip(t *testing.T) {
	s, _ := newTestServer(t)
	_ = setupFuzzTarget(t, s, "api.empresa.com")

	// set
	out, err := callTool(t, s, "set_match_replace", map[string]any{
		"rules": []any{
			map[string]any{"name": "v1->v2", "part": "url", "match": "/v1/", "replace": "/v2/", "enabled": true},
			map[string]any{"part": "req_header", "header": "X-Role", "match": ".*", "replace": "admin"},
		},
	})
	if err != nil {
		t.Fatalf("set_match_replace: %v", err)
	}
	if !strings.Contains(out, `"rules_count":2`) {
		t.Errorf("set out = %s, want rules_count 2", out)
	}

	// list reflete o persistido
	out, err = callTool(t, s, "list_match_replace", map[string]any{})
	if err != nil {
		t.Fatalf("list_match_replace: %v", err)
	}
	if !strings.Contains(out, `"part":"url"`) || !strings.Contains(out, `"replace":"admin"`) {
		t.Errorf("list out = %s, want as 2 regras", out)
	}

	// regex invalida -> erro na validacao do UpdateTarget
	_, err = callTool(t, s, "set_match_replace", map[string]any{
		"rules": []any{map[string]any{"part": "url", "match": "(", "replace": "x"}},
	})
	if err == nil {
		t.Fatal("esperado erro: regex invalida")
	}

	// [] limpa
	_, err = callTool(t, s, "set_match_replace", map[string]any{"rules": []any{}})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	out, _ = callTool(t, s, "list_match_replace", map[string]any{})
	if !strings.Contains(out, `"rules":[]`) && !strings.Contains(out, `"rules":null`) {
		t.Errorf("apos clear list = %s, want vazio", out)
	}
}

func TestSetMatchReplace_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "set_match_replace", map[string]any{"rules": []any{}})
	if err == nil {
		t.Fatal("esperado erro sem alvo ativo")
	}
}
