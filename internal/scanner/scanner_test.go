package scanner

import (
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

func req(method, rawurl string, status int, respBody, respCT string, respHeaders map[string][]string) *store.Request {
	return &store.Request{
		Method: method, URL: rawurl, Status: status,
		RespBody: []byte(respBody),
		RespHeaders: mergeHeaders(respHeaders, map[string][]string{"Content-Type": {respCT}}),
	}
}

func mergeHeaders(a, b map[string][]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestDetectReflectedXSS(t *testing.T) {
	r := req("GET", "https://api.example/search?q=<script>alert(1)</script>", 200,
		`<html>result for <script>alert(1)</script></html>`, "text/html", nil)
	dets := runDetectors(r, "api.example")
	for _, d := range dets {
		if d.Type == "XSS" {
			return
		}
	}
	t.Fatalf("XSS nao detectado: %+v", dets)
}

func TestDetectSQLi(t *testing.T) {
	r := req("GET", "https://api.example/login", 500,
		`You have an error in your SQL syntax near 'select'`, "text/plain", nil)
	dets := runDetectors(r, "api.example")
	for _, d := range dets {
		if d.Type == "SQLi" {
			return
		}
	}
	t.Fatalf("SQLi nao detectado: %+v", dets)
}

func TestDetectSecretInJS(t *testing.T) {
	r := req("GET", "https://api.example/app.js", 200,
		`const key = "AKIAIOSFODNN7EXAMPLE";`, "application/javascript", nil)
	dets := runDetectors(r, "api.example")
	for _, d := range dets {
		if d.Type == "secret" {
			return
		}
	}
	t.Fatalf("secret nao detectado: %+v", dets)
}

func TestDetectOpenRedirect(t *testing.T) {
	r := req("GET", "https://api.example/go?next=https://evil.example", 302,
		"", "text/html", map[string][]string{"Location": {"https://evil.example"}})
	dets := runDetectors(r, "api.example")
	for _, d := range dets {
		if d.Type == "redirect" {
			return
		}
	}
	t.Fatalf("redirect nao detectado: %+v", dets)
}

func TestBuildSitemap(t *testing.T) {
	reqs := []*store.Request{
		req("GET", "https://api.example/users/123", 200, "", "text/html", nil),
		req("GET", "https://api.example/users/456", 200, "", "text/html", nil),
		req("GET", "https://api.example/health", 200, "", "text/html", nil),
	}
	smap := BuildSitemap(reqs)
	found := map[string]int{}
	for _, n := range smap {
		found[n.Path] += n.Hits
	}
	if found["/users/{id}"] != 2 {
		t.Fatalf("sitemap users collapsed: %+v", smap)
	}
	if found["/health"] != 1 {
		t.Fatalf("sitemap health: %+v", smap)
	}
}

func TestRunPassiveCounts(t *testing.T) {
	reqs := []*store.Request{
		req("GET", "https://api.example/search?q=<b>", 200, `<b>x</b>`, "text/html", nil),
		req("GET", "https://api.example/ok", 200, `{"a":1}`, "application/json", nil),
	}
	j := RunPassive(reqs, "api.example")
	if j.Total != 2 {
		t.Fatalf("total=%d", j.Total)
	}
	if j.Hits["XSS"] != 1 {
		t.Fatalf("xss hits=%d: %+v", j.Hits["XSS"], j.Hits)
	}
}
