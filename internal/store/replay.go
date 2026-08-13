package store

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrOutOfScope indica que o host final do replay nao passou no scope guard.
var ErrOutOfScope = errors.New("replay: host fora de escopo")

// Replay re-executa um request capturado (com overrides opcionais), valida o
// host final via scopeMatch antes de enviar e grava o resultado como um novo
// Request. Se a execucao falhar, nada e persistido.
func (s *SQLiteStore) Replay(id int64, overrides ReplayOverrides, scopeMatch func(string) bool) (*ReplayResult, error) {
	orig, err := s.Get(id)
	if err != nil {
		return nil, fmt.Errorf("replay: request %d not found: %w", id, err)
	}
	// Fail-closed: scopeMatch e obrigatorio em caminho security-critical.
	if scopeMatch == nil {
		return nil, errors.New("replay: scopeMatch é nil — recusando executar")
	}

	method := overrides.MethodOverride
	if method == "" {
		method = orig.Method
	}

	rawURL := overrides.URLOverride
	if rawURL == "" {
		rawURL = orig.URL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("replay: url invalida %q: %w", rawURL, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("replay: url sem host: %q", rawURL)
	}

	// Scope guard aplica-se ao host FINAL (pos-urlOverride). Tambem valido sem
	// urlOverride: defense-in-depth sobre a URL original.
	if !scopeMatch(u.Hostname()) {
		return nil, ErrOutOfScope
	}

	headers := http.Header{}
	for k, vs := range orig.ReqHeaders {
		for _, v := range vs {
			headers.Add(k, v)
		}
	}
	for k, v := range overrides.HeaderOverrides {
		headers.Set(k, v)
	}

	body := orig.ReqBody
	if overrides.BodyOverride != nil {
		body = overrides.BodyOverride
	}

	req, err := http.NewRequestWithContext(context.Background(), method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("replay: montar request: %w", err)
	}
	req.Header = headers

	// InsecureSkipVerify hardcoded: store.Replay nao acessa o proxy (decisao
	// aprovada 2026-08-12); strict upstream vira config na Task 26.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	client := &http.Client{Transport: transport}
	if !overrides.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		// Re-guard: cada redirecionamento tambem passa pelo scope guard, para
		// nao seguir para host fora de escopo (gap de seguranca criticado).
		client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
			if !scopeMatch(req.URL.Hostname()) {
				return fmt.Errorf("redirect para host fora de escopo: %w", ErrOutOfScope)
			}
			return nil
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("replay: executar: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("replay: ler resposta: %w", err)
	}

	newID, err := s.Insert(&Request{
		Ts:          time.Now().UnixMilli(),
		Method:      method,
		URL:         u.String(),
		ReqHeaders:  headers,
		ReqBody:     body,
		Status:      resp.StatusCode,
		RespHeaders: resp.Header,
		RespBody:    respBody,
		RespLen:     len(respBody),
	})
	if err != nil {
		return nil, fmt.Errorf("replay: persistir: %w", err)
	}
	return &ReplayResult{NewRequestID: newID, Status: resp.StatusCode, RespLen: len(respBody)}, nil
}
