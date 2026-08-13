package store

import (
	"strings"
	"testing"
)

func TestDiffRequests_BodyUnified(t *testing.T) {
	s := newTestStore(t)
	idA, err := s.Insert(&Request{
		Ts: 1, Method: "POST", URL: "https://x.com/login",
		RespBody: []byte("{\"ok\": true}\n{\"token\": \"abc\"}\n{\"user\": \"alice\"}\n"),
	})
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	idB, err := s.Insert(&Request{
		Ts: 2, Method: "POST", URL: "https://x.com/login",
		RespBody: []byte("{\"ok\": true}\n{\"token\": \"def\"}\n{\"user\": \"alice\"}\n{\"extra\": true}\n"),
	})
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}

	d, err := s.DiffRequests(idA, idB, "resp")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if d == nil {
		t.Fatal("nil diff")
	}
	if d.Mode != "resp" {
		t.Errorf("mode = %q, want resp", d.Mode)
	}
	joined := strings.Join(d.Lines, "\n")
	for _, want := range []string{
		` {"ok": true}`,
		`-{"token": "abc"}`,
		`+{"token": "def"}`,
		` {"user": "alice"}`,
		`+{"extra": true}`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("diff sem linha %q\n%s", want, joined)
		}
	}
	if len(d.ChangedAB) != 1 || d.ChangedAB[0] != `{"token": "abc"}` {
		t.Errorf("ChangedAB = %v, want so {\"token\": \"abc\"}", d.ChangedAB)
	}
	if len(d.ChangedBA) != 2 || d.ChangedBA[0] != `{"token": "def"}` {
		t.Errorf("ChangedBA = %v, want 2 linhas com token def primeiro", d.ChangedBA)
	}
}

func TestDiffRequests_SameBodies_NoChanges(t *testing.T) {
	s := newTestStore(t)
	body := []byte("a\nb\nc\n")
	idA, err := s.Insert(&Request{Ts: 1, Method: "GET", URL: "https://x.com/", RespBody: body})
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	idB, err := s.Insert(&Request{Ts: 2, Method: "GET", URL: "https://x.com/", RespBody: body})
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}
	d, err := s.DiffRequests(idA, idB, "resp")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(d.ChangedAB) != 0 || len(d.ChangedBA) != 0 {
		t.Errorf("bodies iguais: ChangedAB=%v ChangedBA=%v", d.ChangedAB, d.ChangedBA)
	}
	for _, l := range d.Lines {
		if strings.HasPrefix(l, "-") || strings.HasPrefix(l, "+") {
			t.Errorf("bodies iguais com linha de mudanca: %q", l)
		}
	}
}

func TestDiffRequests_ModeReqAndHeaders(t *testing.T) {
	s := newTestStore(t)
	idA, err := s.Insert(&Request{
		Ts: 1, Method: "POST", URL: "https://x.com/",
		ReqBody:     []byte("user=alice\n"),
		ReqHeaders:  map[string][]string{"X-Token": {"a"}},
		RespHeaders: map[string][]string{"Content-Type": {"application/json"}},
	})
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	idB, err := s.Insert(&Request{
		Ts: 2, Method: "POST", URL: "https://x.com/",
		ReqBody:     []byte("user=bob\n"),
		ReqHeaders:  map[string][]string{"X-Token": {"b"}},
		RespHeaders: map[string][]string{"Content-Type": {"application/json"}},
	})
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}

	dr, err := s.DiffRequests(idA, idB, "req")
	if err != nil {
		t.Fatalf("diff req: %v", err)
	}
	if strings.Join(dr.Lines, "\n") != "-user=alice\n+user=bob" {
		t.Errorf("diff req = %q", strings.Join(dr.Lines, "\n"))
	}

	dh, err := s.DiffRequests(idA, idB, "headers")
	if err != nil {
		t.Fatalf("diff headers: %v", err)
	}
	joined := strings.Join(dh.Lines, "\n")
	if !strings.Contains(joined, "-x-token: a") || !strings.Contains(joined, "+x-token: b") {
		t.Errorf("diff headers sem x-token:\n%s", joined)
	}
	if strings.Contains(joined, "content-type") {
		// header inalterado so pode aparecer como linha de contexto (" "), nunca +/-.
		for _, l := range dh.Lines {
			if strings.Contains(l, "content-type") && (strings.HasPrefix(l, "-") || strings.HasPrefix(l, "+")) {
				t.Errorf("diff headers marca header inalterado como mudanca: %q\n%s", l, joined)
			}
		}
	}
}

func TestDiffRequests_Errors(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Insert(&Request{Ts: 1, Method: "GET", URL: "https://x.com/", RespBody: []byte("x\n")})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := s.DiffRequests(9999, id, "resp"); err == nil {
		t.Error("id inexistente: esperado erro")
	}
	if _, err := s.DiffRequests(id, 9999, "resp"); err == nil {
		t.Error("id inexistente: esperado erro")
	}
	if _, err := s.DiffRequests(id, id, "bogus"); err == nil {
		t.Error("mode invalido: esperado erro")
	}
}
