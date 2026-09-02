package scanner

import (
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

func TestIsDestructive(t *testing.T) {
	cases := map[string]bool{
		"<script>alert(1)</script>": false,
		"'; DROP TABLE users; --":    true,
		"'; rm -rf / --":             true,
		"http://127.0.0.1":           false,
	}
	for p, want := range cases {
		if got := isDestructive(p); got != want {
			t.Errorf("isDestructive(%q)=%v want %v", p, got, want)
		}
	}
}

func TestBuildActiveRequests_CapsAndSkipsNonFuzzable(t *testing.T) {
	reqs := []*store.Request{
		{ID: 1, Method: "GET", URL: "https://api.example.com/search?q=a"},   // fuzzavel
		{ID: 2, Method: "GET", URL: "https://api.example.com/static/x.css"},  // sem query/body
		{ID: 3, Method: "POST", URL: "https://api.example.com/login", ReqBody: []byte("u=1")}, // fuzzavel
	}
	ars := BuildActiveRequests(reqs, 0)
	if len(ars) == 0 {
		t.Fatalf("esperava active requests, got none")
	}
	for _, ar := range ars {
		if ar.Payload == "" {
			t.Fatalf("payload vazio em %+v", ar)
		}
		if stringsContains(ar.Payload, "DROP") {
			t.Fatalf("payload destrutivo vazou: %q", ar.Payload)
		}
	}
	// com cap de 1, retorna so 1
	ars1 := BuildActiveRequests(reqs, 1)
	if len(ars1) != 1 {
		t.Fatalf("cap=1 deve retornar 1, got %d", len(ars1))
	}
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
