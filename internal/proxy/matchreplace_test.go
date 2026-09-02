package proxy

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/projects"
)

func newReq(t *testing.T, method, rawURL, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

// scope que aceita qualquer host (rewrites nao saem de escopo).
func anyScope(*url.URL) bool { return true }

func TestApplyMatchReplace_URL(t *testing.T) {
	req := newReq(t, "GET", "https://api.empresa.com/v1/users", "")
	rules := []projects.MatchReplaceRule{
		{Part: "url", Match: `/v1/`, Replace: "/v2/", Enabled: true},
	}
	applyMatchReplace(req, rules, anyScope, nil)
	if req.URL.String() != "https://api.empresa.com/v2/users" {
		t.Errorf("url = %q, want .../v2/users", req.URL.String())
	}
}

func TestApplyMatchReplace_URLOutOfScopeReverted(t *testing.T) {
	req := newReq(t, "GET", "https://api.empresa.com/x", "")
	rules := []projects.MatchReplaceRule{
		{Part: "url", Match: `api\.empresa\.com`, Replace: "evil.example", Enabled: true},
	}
	// scope so aceita empresa.com -> rewrite p/ evil.example deve ser revertido
	scope := func(u *url.URL) bool { return strings.HasSuffix(u.Hostname(), "empresa.com") }
	applyMatchReplace(req, rules, scope, nil)
	if req.URL.Hostname() != "api.empresa.com" {
		t.Errorf("host = %q, want api.empresa.com (rewrite fora de escopo revertido)", req.URL.Hostname())
	}
}

func TestApplyMatchReplace_Header(t *testing.T) {
	req := newReq(t, "GET", "https://api.empresa.com/", "")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	rules := []projects.MatchReplaceRule{
		{Part: "req_header", Header: "X-Forwarded-For", Match: `.*`, Replace: "127.0.0.1", Enabled: true},
	}
	applyMatchReplace(req, rules, anyScope, nil)
	if got := req.Header.Get("X-Forwarded-For"); got != "127.0.0.1" {
		t.Errorf("XFF = %q, want 127.0.0.1", got)
	}
}

func TestApplyMatchReplace_Body(t *testing.T) {
	req := newReq(t, "POST", "https://api.empresa.com/", `{"role":"user"}`)
	rules := []projects.MatchReplaceRule{
		{Part: "req_body", Match: `"role":"user"`, Replace: `"role":"admin"`, Enabled: true},
	}
	applyMatchReplace(req, rules, anyScope, nil)
	b, _ := io.ReadAll(req.Body)
	if string(b) != `{"role":"admin"}` {
		t.Errorf("body = %q, want role admin", string(b))
	}
	if req.ContentLength != int64(len(b)) {
		t.Errorf("ContentLength = %d, want %d", req.ContentLength, len(b))
	}
}

func TestApplyMatchReplace_DisabledSkipped(t *testing.T) {
	req := newReq(t, "GET", "https://api.empresa.com/v1/x", "")
	rules := []projects.MatchReplaceRule{
		{Part: "url", Match: `/v1/`, Replace: "/v2/", Enabled: false},
	}
	applyMatchReplace(req, rules, anyScope, nil)
	if !strings.Contains(req.URL.String(), "/v1/") {
		t.Errorf("regra desabilitada nao deve aplicar: %q", req.URL.String())
	}
}
