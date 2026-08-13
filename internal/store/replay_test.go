package store

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestReplay_HonorsScopeGuard: replay com urlOverride para host fora de escopo
// -> ErrOutOfScope e nada persistido; host dentro -> 200 e novo id gravado.
func TestReplay_HonorsScopeGuard(t *testing.T) {
	s := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	}))
	defer upstream.Close()

	origID, err := s.Insert(&Request{
		Ts: 1, Method: "GET", URL: "https://api.empresa.com/users",
		ReqHeaders: map[string][]string{"X-Orig": {"1"}},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	upURL, _ := url.Parse(upstream.URL)
	scopeMatch := func(host string) bool { return host == upURL.Hostname() }

	// Fora de escopo: urlOverride para host que scopeMatch rejeita.
	_, err = s.Replay(origID, ReplayOverrides{URLOverride: "https://evil.example.com/steal"}, scopeMatch)
	if !errors.Is(err, ErrOutOfScope) {
		t.Fatalf("out-of-scope: err = %v, want ErrOutOfScope", err)
	}
	if n, _ := s.Count(); n != 1 {
		t.Errorf("out-of-scope persistiu replay: count = %d, want 1", n)
	}

	// Dentro de escopo: urlOverride aponta para o upstream httptest.
	res, err := s.Replay(origID, ReplayOverrides{URLOverride: upstream.URL + "/replay"}, scopeMatch)
	if err != nil {
		t.Fatalf("in-scope replay: %v", err)
	}
	if res.Status != http.StatusOK || res.RespLen != 4 {
		t.Errorf("result = %+v, want status 200 resp_len 4", res)
	}
	if res.NewRequestID == origID || res.NewRequestID == 0 {
		t.Fatalf("new id = %d, want novo id != %d", res.NewRequestID, origID)
	}
	got, err := s.Get(res.NewRequestID)
	if err != nil {
		t.Fatalf("get replayed: %v", err)
	}
	if got.URL != upstream.URL+"/replay" || got.Status != http.StatusOK || got.RespLen != 4 {
		t.Errorf("replayed record = %+v, want url/status/resp_len do replay", got)
	}
}

// TestReplay_NoOverrideUsesOriginalURL: sem urlOverride, replay usa a URL
// original (aqui o upstream), ainda passando pelo scope guard.
func TestReplay_NoOverrideUsesOriginalURL(t *testing.T) {
	s := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	origID, err := s.Insert(&Request{
		Ts: 1, Method: "GET", URL: upstream.URL + "/orig", ReqHeaders: map[string][]string{},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Defense-in-depth: mesmo host original capturado, scopeMatch false barra.
	_, err = s.Replay(origID, ReplayOverrides{}, func(string) bool { return false })
	if !errors.Is(err, ErrOutOfScope) {
		t.Fatalf("no-override out-of-scope: err = %v, want ErrOutOfScope", err)
	}
	if n, _ := s.Count(); n != 1 {
		t.Errorf("count = %d, want 1 (nada persistido)", n)
	}

	// scopeMatch true: re-executa e persiste com a URL original.
	res, err := s.Replay(origID, ReplayOverrides{}, func(string) bool { return true })
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", res.Status)
	}
	got, err := s.Get(res.NewRequestID)
	if err != nil || got.URL != upstream.URL+"/orig" {
		t.Errorf("replayed url = %+v (%v), want original", got, err)
	}
}

// TestReplay_OverridesApplied: method/header/body overrides chegam ao upstream.
func TestReplay_OverridesApplied(t *testing.T) {
	s := newTestStore(t)
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

	origID, err := s.Insert(&Request{
		Ts: 1, Method: "GET", URL: upstream.URL, ReqHeaders: map[string][]string{"X-New": {"old"}},
		ReqBody: []byte("corpo-original"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	res, err := s.Replay(origID, ReplayOverrides{
		MethodOverride:   "POST",
		HeaderOverrides:  map[string]string{"X-New": "novo"},
		BodyOverride:     []byte("corpo-novo"),
		FollowRedirects:  false,
		URLOverride:      upstream.URL + "/override",
	}, func(string) bool { return true })
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res.Status != http.StatusNoContent {
		t.Errorf("status = %d, want 204", res.Status)
	}
	if gotMethod != "POST" || gotXNew != "novo" || gotBody != "corpo-novo" {
		t.Errorf("upstream recebeu method=%q x-new=%q body=%q, want POST/novo/corpo-novo", gotMethod, gotXNew, gotBody)
	}
	// O header original foi substituido (nao somado) e persistido.
	got, err := s.Get(res.NewRequestID)
	if err != nil {
		t.Fatalf("get replayed: %v", err)
	}
	if hv := got.ReqHeaders["X-New"]; len(hv) != 1 || hv[0] != "novo" {
		t.Errorf("header persistido = %v, want [novo]", hv)
	}
}

// TestReplay_FollowRedirects: false para no 3xx; true segue ate o 2xx.
func TestReplay_FollowRedirects(t *testing.T) {
	s := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("final"))
	}))
	defer upstream.Close()

	origID, err := s.Insert(&Request{Ts: 1, Method: "GET", URL: upstream.URL + "/start", ReqHeaders: map[string][]string{}})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	res, err := s.Replay(origID, ReplayOverrides{}, func(string) bool { return true })
	if err != nil {
		t.Fatalf("replay no-follow: %v", err)
	}
	if res.Status != http.StatusFound {
		t.Errorf("FollowRedirects=false: status = %d, want 302", res.Status)
	}

	res, err = s.Replay(origID, ReplayOverrides{FollowRedirects: true}, func(string) bool { return true })
	if err != nil {
		t.Fatalf("replay follow: %v", err)
	}
	if res.Status != http.StatusOK {
		t.Errorf("FollowRedirects=true: status = %d, want 200", res.Status)
	}
	got, err := s.Get(res.NewRequestID)
	if err != nil || string(got.RespBody) != "final" {
		t.Errorf("seguido: resp_body = %q (%v), want final", got.RespBody, err)
	}
}

// TestReplay_RedirectOutOfScopeAborts: redirect (3xx) para host fora de escopo
// aborta a cadeia; erro ErrOutOfScope e nada persistido.
func TestReplay_RedirectOutOfScopeAborts(t *testing.T) {
	s := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example.com/steal", http.StatusFound)
	}))
	defer upstream.Close()

	upURL, _ := url.Parse(upstream.URL)
	scopeMatch := func(host string) bool { return host == upURL.Hostname() }

	origID, err := s.Insert(&Request{Ts: 1, Method: "GET", URL: upstream.URL, ReqHeaders: map[string][]string{}})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, err = s.Replay(origID, ReplayOverrides{FollowRedirects: true}, scopeMatch)
	if err == nil || !errors.Is(err, ErrOutOfScope) {
		t.Fatalf("err = %v, want ErrOutOfScope no redirect fora de escopo", err)
	}
	if n, _ := s.Count(); n != 1 {
		t.Errorf("count = %d, want 1 (replay abortado nao persistido)", n)
	}
}

// TestReplay_NilScopeMatchFailsClosed: scopeMatch nil recusa executar.
func TestReplay_NilScopeMatchFailsClosed(t *testing.T) {
	s := newTestStore(t)
	origID, err := s.Insert(&Request{Ts: 1, Method: "GET", URL: "https://x.com/", ReqHeaders: map[string][]string{}})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	_, err = s.Replay(origID, ReplayOverrides{}, nil)
	if err == nil {
		t.Fatal("nil scopeMatch deve falhar fechado, nao enviar")
	}
	if n, _ := s.Count(); n != 1 {
		t.Errorf("count = %d, want 1 (nada persistido)", n)
	}
}

// TestReplay_MissingID: id inexistente -> erro, sem pouso no scope guard.
func TestReplay_MissingID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Replay(999999, ReplayOverrides{}, func(string) bool { return true })
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if errors.Is(err, ErrOutOfScope) {
		t.Fatalf("err = %v, nao pode ser ErrOutOfScope", err)
	}
	if !strings.Contains(err.Error(), "999999") {
		t.Errorf("err = %v, deve citar o id", err)
	}
}

// TestReplay_NetworkError_NoPersist: falha de execucao retorna erro e nao
// grava record parcial.
func TestReplay_NetworkError_NoPersist(t *testing.T) {
	s := newTestStore(t)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	origID, err := s.Insert(&Request{Ts: 1, Method: "GET", URL: "http://127.0.0.1:1/x", ReqHeaders: map[string][]string{}})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, err = s.Replay(origID, ReplayOverrides{URLOverride: deadURL}, func(string) bool { return true })
	if err == nil {
		t.Fatal("expected network error")
	}
	if n, _ := s.Count(); n != 1 {
		t.Errorf("count = %d, want 1 (replay falho nao persistido)", n)
	}
}
