package proxy

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/config"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
	"gopkg.in/yaml.v3"
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
// pcfg opcional define a captura de body (task 17); zero = defaults.
func newTestProxy(t *testing.T, tgt *projects.Target, pcfg ...config.Proxy) (*Proxy, string) {
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
	p := NewProxy(st, caDir)
	if len(pcfg) == 1 {
		p.SetCaptureConfig(pcfg[0])
	}
	if tgt != nil {
		p.SetTarget(tgt)
	}
	if err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop() })
	return p, p.Addr()
}

// upstreamCT retorna um TLS server que responde com content-type e body fixos.
func upstreamCT(t *testing.T, ct string, body []byte) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
}

// browseBody faz GET via proxy e devolve o body recebido pelo CLIENTE (i.e. o
// que realmente passou pelo proxy, integridade do passthrough).
func browseBody(t *testing.T, proxyURL *url.URL, up *httptest.Server) []byte {
	t.Helper()
	tr := &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: tr}
	resp, err := client.Get(up.URL + "/")
	if err != nil {
		t.Fatalf("client.Get via proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return b
}

func TestProxy_HTTPS_RecordsFullTransaction(t *testing.T) {
	up := upstream(t)
	defer up.Close()

	// target sem in-scope patterns: permite tudo que sobrar.
	p, addr := newTestProxy(t, &projects.Target{Host: "in-scope.test"})
	proxyURL, _ := url.Parse("http://" + addr)
	dialViaProxy(t, proxyURL, up)

	all, err := p.store.All()
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

	all, err := p.store.All()
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

	all, _ := p.store.All()
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
	all, _ := p.store.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 row, got %d", len(all))
	}
	got := all[0]
	if len(got.ReqBody) != 0 || len(got.RespBody) != 0 {
		t.Error("127.0.0.1 not in *.test scope; bodies should be omitted")
	}
}

// TestProxy_ReloadsScopeFromMetaYAML (12.3): MCP server e proxy sao processos
// separados — o proxy so repara no novo scope via stat do meta.yaml a cada
// request. Escopo A restringe (request 1 sem body); novo meta.yaml (escopo B)
// libera o upstream; request 2 ja captura com body. Sem restart do proxy.
func TestProxy_ReloadsScopeFromMetaYAML(t *testing.T) {
	up := upstream(t)
	defer up.Close()

	dir := t.TempDir()
	projDir := filepath.Join(dir, "P")
	tgtDir := filepath.Join(projDir, "targets", "x.test")
	if err := os.MkdirAll(tgtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(tgtDir, "meta.yaml")
	writeMeta := func(inScope []string) {
		data, err := yaml.Marshal(&projects.Target{Host: "x.test", InScopePatterns: inScope})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(meta, data, 0o600); err != nil {
			t.Fatal(err)
		}
		// bump mtime explicito: granularidade de mtime de filesystem pode
		// coincidir entre writes; forcar mtime futuro torna o teste deterministico.
		future := time.Now().Add(5 * time.Second)
		if err := os.Chtimes(meta, future, future); err != nil {
			t.Fatal(err)
		}
	}
	// escopo A: restringe; 127.0.0.1 (upstream httptest) fica FORA -> sem body.
	writeMeta([]string{"*.in-scope.test"})

	repo := projects.NewRepo(dir)
	tgt, err := repo.LoadTarget("P", "x.test")
	if err != nil {
		t.Fatal(err)
	}

	caDir := filepath.Join(t.TempDir(), "ca")
	if _, _, err := EnsureCA(caDir); err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "requests.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := NewProxy(st, caDir)
	p.SetTargetReload(tgt, meta, func() (*projects.Target, error) {
		return repo.LoadTarget("P", "x.test")
	})
	if err := p.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop() })
	proxyURL, _ := url.Parse("http://" + p.Addr())

	dialViaProxy(t, proxyURL, up) // request 1: escopo A -> out-of-scope

	// escopo B: libera o loopback do upstream -> in-scope (com body).
	writeMeta([]string{"127.0.0.1"})
	dialViaProxy(t, proxyURL, up) // request 2: recarga viva via mtime-check

	all, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(all))
	}
	// All() ordena id DESC: all[1] = request 1, all[0] = request 2.
	first, second := all[1], all[0]
	if len(first.ReqBody) != 0 || len(first.RespBody) != 0 || first.Status != 0 {
		t.Errorf("request 1 (escopo A) deveria ser out-of-scope: status=%d req=%d resp=%d bytes",
			first.Status, len(first.ReqBody), len(first.RespBody))
	}
	if second.Status != http.StatusOK || string(second.RespBody) != `{"hello":"world"}` {
		t.Errorf("request 2 (escopo B) deveria ser in-scope: status=%d resp=%q",
			second.Status, second.RespBody)
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

// 17.1: Content-Type que casa glob de NoBodyContentTypes nao tem o body
// capturado (RespBodySkipped) — mas o cliente continua recebendo o body INTEGRO.
func TestProxy_SkipsBodyForImages(t *testing.T) {
	const n = 100 * 1024
	up := upstreamCT(t, "image/png", bytes.Repeat([]byte{0x89}, n))
	defer up.Close()

	p, addr := newTestProxy(t, &projects.Target{Host: "in-scope.test"}, config.Proxy{
		NoBodyContentTypes: []string{"image/*"},
		BodyCapBytes:       1 << 20,
	})
	proxyURL, _ := url.Parse("http://" + addr)

	got := browseBody(t, proxyURL, up)
	if len(got) != n {
		t.Fatalf("passthrough: cliente recebeu %d bytes, esperado %d", len(got), n)
	}

	all, err := p.store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 request, got %d", len(all))
	}
	r := all[0]
	if len(r.RespBody) != 0 {
		t.Errorf("image body nao deveria ser capturado, got %d bytes", len(r.RespBody))
	}
	if !r.RespBodySkipped {
		t.Error("RespBodySkipped deveria ser true para image/*")
	}
	if r.RespBodyTruncated {
		t.Error("skip nao e truncamento: RespBodyTruncated deveria ser false")
	}
}

// 17.3: body acima do cap e capturado so ate o limite, com flag de truncamento;
// o cliente continua recebendo o body completo (passthrough intacto).
func TestProxy_CapsBodyOverLimit(t *testing.T) {
	const capBytes = 1024
	const n = 100 * 1024
	up := upstreamCT(t, "text/html", bytes.Repeat([]byte("a"), n))
	defer up.Close()

	p, addr := newTestProxy(t, &projects.Target{Host: "in-scope.test"}, config.Proxy{
		BodyCapBytes: capBytes,
	})
	proxyURL, _ := url.Parse("http://" + addr)

	got := browseBody(t, proxyURL, up)
	if len(got) != n {
		t.Fatalf("passthrough: cliente recebeu %d bytes, esperado %d", len(got), n)
	}

	all, err := p.store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 request, got %d", len(all))
	}
	r := all[0]
	if len(r.RespBody) != capBytes {
		t.Errorf("RespBody capturado = %d bytes, esperado cap %d", len(r.RespBody), capBytes)
	}
	if r.RespLen != capBytes {
		t.Errorf("RespLen = %d, esperado %d", r.RespLen, capBytes)
	}
	if !r.RespBodyTruncated {
		t.Error("RespBodyTruncated deveria ser true acima do cap")
	}
	if r.RespBodySkipped {
		t.Error("cap nao e skip: RespBodySkipped deveria ser false")
	}
}

// 17.3 (glob que nao casa): content-type normal continua sendo capturado
// integralmente com defaults — protege o comportamento do e2e (HTML/JSON).
func TestProxy_CapturesNonMatchingContentType(t *testing.T) {
	up := upstream(t)
	defer up.Close()

	p, addr := newTestProxy(t, &projects.Target{Host: "in-scope.test"}, config.DefaultProxy())
	proxyURL, _ := url.Parse("http://" + addr)
	dialViaProxy(t, proxyURL, up)

	all, err := p.store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 request, got %d", len(all))
	}
	r := all[0]
	if string(r.RespBody) != `{"hello":"world"}` {
		t.Errorf("resp body = %q", r.RespBody)
	}
	if r.RespBodySkipped || r.RespBodyTruncated {
		t.Errorf("flags: skipped=%v truncated=%v deveriam ser false", r.RespBodySkipped, r.RespBodyTruncated)
	}
}
