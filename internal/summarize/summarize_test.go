package summarize

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestSummarize_HTML_ExtractsFormsAndLinks(t *testing.T) {
	htmlBody := `<!doctype html>
<html><body>
  <form action="/login" method="POST">
    <input type="text" name="user">
    <input type="hidden" name="csrf" value="abc123">
    <input type="password" name="pass">
  </form>
  <form action="/search"><input name="q"></form>
  <a href="/profile">Profile</a>
  <a href="https://api.empresa.com/v1/users">API</a>
  <a href="/profile">dup link</a>
  <script src="/static/app.js"></script>
  <script>var apiKey = "AKIAIOSFODNN7EXAMPLE";</script>
  <!-- TODO: remove hardcoded creds -->
</body></html>`

	res := Body("text/html; charset=utf-8", []byte(htmlBody), 1<<20)
	if res.Kind != "html" {
		t.Fatalf("kind = %q, want html", res.Kind)
	}
	if res.Truncated {
		t.Error("body < max: truncated should be false")
	}
	forms := res.Detail["forms"].([]FormInfo)
	if len(forms) != 2 {
		t.Fatalf("forms = %d, want 2: %+v", len(forms), forms)
	}
	if forms[0].Action != "/login" || forms[0].Method != "POST" {
		t.Errorf("form[0] = %+v, want action=/login method=POST", forms[0])
	}
	for _, f := range []string{"user", "csrf", "pass"} {
		if !slices.Contains(forms[0].Fields, f) {
			t.Errorf("form[0].fields = %v, want %q", forms[0].Fields, f)
		}
	}
	if forms[1].Action != "/search" || forms[1].Method != "GET" {
		t.Errorf("form[1] = %+v, want action=/search method=GET (default)", forms[1])
	}
	links := res.Detail["links"].([]string)
	if len(links) != 2 {
		t.Fatalf("links = %v, want 2 deduped", links)
	}
	for _, l := range []string{"/profile", "https://api.empresa.com/v1/users"} {
		if !slices.Contains(links, l) {
			t.Errorf("links = %v, want %q", links, l)
		}
	}
	ext := res.Detail["scripts_external"].([]string)
	if !slices.Contains(ext, "/static/app.js") {
		t.Errorf("scripts_external = %v, want /static/app.js", ext)
	}
	if n := res.Detail["scripts_inline"].(int); n != 1 {
		t.Errorf("scripts_inline = %d, want 1", n)
	}
	comments := res.Detail["comments"].([]string)
	if len(comments) != 1 || !strings.Contains(comments[0], "TODO") {
		t.Errorf("comments = %v, want 1 interesting TODO comment", comments)
	}
}

func TestSummarize_JSON_WalksKeysAndTypes_NoValues(t *testing.T) {
	body := `{"user":{"name":"alice","id":42,"active":true,"tags":["a","b"],"meta":null,"addr":{"street":"x"}},"deep":{"l1":{"l2":{"l3":"hidden"}}}}`
	res := Body("application/json", []byte(body), 1<<20)
	if res.Kind != "json" {
		t.Fatalf("kind = %q, want json", res.Kind)
	}
	if res.Note != "" {
		t.Errorf("note = %q, want empty", res.Note)
	}
	keys, ok := res.Detail["keys"].(map[string]any)
	if !ok {
		t.Fatalf("keys missing: %+v", res.Detail)
	}
	user, _ := keys["user"].(map[string]any)
	if user["name"] != "string" || user["id"] != "number" || user["active"] != "boolean" || user["meta"] != "null" {
		t.Errorf("user = %v, want name=string id=number active=boolean meta=null", user)
	}
	if tags, ok := user["tags"].([]any); !ok || len(tags) != 1 || tags[0] != "string" {
		t.Errorf("tags = %v, want [string] (types only, no values)", user["tags"])
	}
	if addr, ok := user["addr"].(map[string]any); !ok || addr["street"] != "string" {
		t.Errorf("addr = %v, want expanded {street:string}", user["addr"])
	}
	deep, _ := keys["deep"].(map[string]any)
	l1, _ := deep["l1"].(map[string]any)
	if l2, ok := l1["l2"].(string); !ok || l2 != "object" {
		t.Errorf("l1.l2 = %v, want collapsed 'object' at depth cap", l1["l2"])
	}
	b, _ := json.Marshal(res.Detail)
	for _, forbidden := range []string{"alice", "42", "hidden", `"a"`} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("value %q leaked into summary: %s", forbidden, b)
		}
	}
}

func TestSummarize_JS_FindsEndpointsCallsAndTokens(t *testing.T) {
	js := `
const api = "https://api.empresa.com/v1/users";
fetch('/api/login', {method: 'POST'});
var xhr = new XMLHttpRequest();
xhr.open('GET', '/api/config', true);
axios.post('/api/orders');
var jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.sig_sig_sig";
`
	res := Body("application/javascript", []byte(js), 1<<20)
	if res.Kind != "js" {
		t.Fatalf("kind = %q, want js", res.Kind)
	}
	urls := res.Detail["urls"].([]string)
	if !slices.Contains(urls, "https://api.empresa.com/v1/users") {
		t.Errorf("urls = %v, want api.empresa.com", urls)
	}
	calls := res.Detail["calls"].([]CallInfo)
	want := []CallInfo{
		{Kind: "fetch", Target: "/api/login"},
		{Kind: "xhr", Target: "/api/config"},
		{Kind: "axios", Target: "/api/orders"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %+v, want %+v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("calls[%d] = %+v, want %+v", i, calls[i], want[i])
		}
	}
	tokens := res.Detail["tokens"].([]TokenInfo)
	if len(tokens) != 1 || tokens[0].Type != "jwt" || !strings.HasPrefix(tokens[0].Hint, "eyJ") {
		t.Errorf("tokens = %+v, want 1 jwt with eyJ hint", tokens)
	}
}

func TestSummarize_JSON_TruncatedPrefixNotesIncompleteness(t *testing.T) {
	body := []byte(`{"a":1}`)
	res := Body("application/json", body, 4)
	if res.Kind != "json" {
		t.Fatalf("kind = %q", res.Kind)
	}
	if !res.Truncated || res.TotalLen != len(body) {
		t.Errorf("truncated=%v total=%d, want true/%d", res.Truncated, res.TotalLen, len(body))
	}
	if !strings.Contains(res.Note, "truncad") {
		t.Errorf("note = %q, want truncation note", res.Note)
	}
}
