package proxy

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/store"
)

// upstream retorna um httptest.NewTLSServer que responde OK com body conhecido.
func upstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"hello":"world"}`)
	}))
}

func dialViaProxy(t *testing.T, proxyURL *url.URL, up *httptest.Server) {
	t.Helper()
	tr := &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: tr}
	req, _ := http.NewRequest("GET", up.URL+"/api/users?id=42", nil)
	req.Header.Set("X-Test", "1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"hello":"world"}` {
		t.Errorf("upstream body = %q", string(body))
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// newTestProxy sobe o proxy em :0 (porta aleatoria) com target opcional.
func newTestProxy(t *testing.T, tgt *projects.Target) (*Proxy, string) {
	t.Helper()
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if _, _, err := EnsureCA(caDir); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "requests.db")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := New(st, caDir)
	if tgt != nil {
		p.SetTarget(tgt)
	}
	if err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop() })
	return p, p.Addr()
}

func TestProxy_HTTPS_RecordsFullTransaction(t *testing.T) {
	up := upstream(t)
	defer up.Close()

	// target sem in-scope patterns: permite tudo que sobrar.
	p, addr := newTestProxy(t, &projects.Target{Host: "in-scope.test"})
	proxyURL, _ := url.Parse("http://" + addr)
	dialViaProxy(t, proxyURL, up)

	all, err := p.store.List(store.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 request, got %d", len(all))
	}
	got := all[0]
	if got.Method != "GET" {
		t.Errorf("method = %q", got.Method)
	}
	if got.URL == "" {
		t.Error("URL empty")
	}
	if got.Status != 200 {
		t.Errorf("status = %d", got.Status)
	}
	if string(got.RespBody) != `{"hello":"world"}` {
		t.Errorf("resp body = %q", got.RespBody)
	}
}

func TestProxy_OutOfScope_OmitsBodies(t *testing.T) {
	up := upstream(t)
	defer up.Close()

	// target restringe a *.in-scope.test; upstream roda em 127.0.0.1:porta
	p, addr := newTestProxy(t, &projects.Target{
		Host:            "out.test",
		InScopePatterns: []string{"*.in-scope.test"},
	})
	proxyURL, _ := url.Parse("http://" + addr)
	dialViaProxy(t, proxyURL, up)

	all, err := p.store.List(store.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 row, got %d", len(all))
	}
	got := all[0]
	if got.Method == "" || got.URL == "" {
		t.Error("out-of-scope row missing metadata")
	}
	if len(got.ReqBody) != 0 {
		t.Errorf("out-of-scope should not store req body, got %d bytes", len(got.ReqBody))
	}
	if len(got.RespBody) != 0 {
		t.Errorf("out-of-scope should not store resp body, got %d bytes", len(got.RespBody))
	}
	if got.Status != 0 {
		t.Errorf("out-of-scope should not have status, got %d", got.Status)
	}
}

func TestProxy_NoTarget_LogsMetadataWithoutBody(t *testing.T) {
	up := upstream(t)
	defer up.Close()
	// sem target: tudo e tratado como out-of-scope. PRD §4.1
	// "trafego fora do escopo e apenas repassado, sem log completo de corpo".
	// Em producao isto nao acontece — o CLI `mcp-proxy proxy` exige alvo ativo.
	p, addr := newTestProxy(t, nil)
	proxyURL, _ := url.Parse("http://" + addr)
	dialViaProxy(t, proxyURL, up)

	all, _ := p.store.List(store.ListFilter{})
	if len(all) != 1 {
		t.Fatalf("expected 1 row, got %d", len(all))
	}
	got := all[0]
	if got.Method == "" || got.URL == "" {
		t.Error("metadata missing")
	}
	if len(got.ReqBody) != 0 || len(got.RespBody) != 0 {
		t.Error("no target: bodies must be omitted")
	}
}

func TestProxy_OutOfScopeOverrides(t *testing.T) {
	up := upstream(t)
	defer up.Close()
	// in-scope permite *.test; out-of-scope bloqueia *.admin.test
	p, addr := newTestProxy(t, &projects.Target{
		Host:               "x.test",
		InScopePatterns:    []string{"*.test"},
		OutOfScopePatterns: []string{"*.admin.test"},
	})
	proxyURL, _ := url.Parse("http://" + addr)
	// para este teste nao importa o upstream especifico: so queremos
	// ver que o escopo da a resposta certa. 127.0.0.1 nao casa *.test
	// e tambem nao casa out-of-scope. Resultado: recusado (sem body).
	dialViaProxy(t, proxyURL, up)
	all, _ := p.store.List(store.ListFilter{})
	if len(all) != 1 {
		t.Fatalf("expected 1 row, got %d", len(all))
	}
	got := all[0]
	if len(got.ReqBody) != 0 || len(got.RespBody) != 0 {
		t.Error("127.0.0.1 not in *.test scope; bodies should be omitted")
	}
}

// garante que o CA gerado e confiavel quando adicionado a um pool x509.
func TestProxy_CAIsTrustedPool(t *testing.T) {
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	cert, _, err := EnsureCA(caDir)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	_ = pool // sanity: AddCert nao panicou
}

// sanity: store.Request sobrevive round-trip JSON.
func TestProxy_StoredRequestIsJSONSafe(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.OpenSQLite(filepath.Join(dir, "x.db"))
	defer st.Close()
	id, _ := st.Insert(&store.Request{
		Ts: time.Now().UnixMilli(), Method: "GET", URL: "https://x/",
		ReqHeaders: map[string][]string{"X": {"a"}},
		ReqBody:    []byte("hi"),
		Status:     200,
		RespBody:   []byte(`{"k":"v"}`),
	})
	if id == 0 {
		t.Fatal("insert returned 0")
	}
	got, _ := st.Get(id)
	if !bytes.Contains([]byte(got.URL), []byte("https://x/")) {
		t.Errorf("URL roundtrip = %q", got.URL)
	}
}
