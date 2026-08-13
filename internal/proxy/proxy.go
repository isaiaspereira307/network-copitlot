package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/store"
)

// Proxy e o MITM HTTP/HTTPS. Reusa o store ja existente para gravar
// transacoes; o escopo vem do Target ativo (PRD §4.1).
//
// Fluxo:
//  1. onRequest: parse method/URL/headers/body, checa escopo, buffer em ProxyCtx.
//  2. onResponse: parse status/headers/body, monta store.Request, Insere.
//
// Conexao fora de escopo: gravamos metadados (method, URL, headers) sem
// body (PRD §4.1 "Escopo: ... sem log completo de corpo").
//
// Upstream TLS: o proxy NAO verifica o cert do upstream por padrao (modo
// pentest — alvos com cert quebrado/self-signed sao o caso comum). Use
// SetStrictUpstream(true) para forcar validacao contra o trust store do
// sistema.
type Proxy struct {
	store  store.Store
	caDir  string
	addr   string
	logger *log.Logger

	mu               sync.Mutex
	target           *projects.Target
	scope            *Scope
	server           *http.Server
	ln               net.Listener
	strictUpstream   bool // false = InsecureSkipVerify no upstream (default pentest)
}

// NewProxy cria um Proxy. caDir e onde EnsureCA persiste/le o CA.
// O proxy so escuta apos Start().
func NewProxy(s store.Store, caDir string) *Proxy {
	return &Proxy{store: s, caDir: caDir, logger: log.Default()}
}

// SetTarget atualiza o target ativo e refaz o escopo.
// Seguro para chamar de qualquer goroutine.
func (p *Proxy) SetTarget(t *projects.Target) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.target = t
	p.scope = New(t)
}

// Target retorna o target ativo (ou nil). Util para diagnosticos.
func (p *Proxy) Target() *projects.Target {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.target
}

// Addr retorna o endereco real de escuta apos Start(). Vazio antes.
func (p *Proxy) Addr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.addr
}

// SetStrictUpstream alterna validacao de cert do upstream. Default: false
// (pentest mode — aceita cert self-signed/expirado do alvo).
func (p *Proxy) SetStrictUpstream(strict bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.strictUpstream = strict
}

// Start liga o proxy no addr (default :8080). Nao-bloqueante: o serve
// fica em goroutine. Requer que SetTarget() tenha sido chamado antes,
// senao tudo sera gravado sem body (escopo vazio = recusa).
func (p *Proxy) Start(addr string) error {
	if addr == "" {
		addr = ":8080"
	}
	cert, key, err := EnsureCA(p.caDir)
	if err != nil {
		return err
	}
	// Constroi a tls.Certificate a partir do nosso CA.
	tlsCert := tls.Certificate{
		Certificate: [][]byte{cert.Raw},
		PrivateKey:  key,
		Leaf:        cert,
	}
	// Action de MITM que usa nosso CA. Equivalente a goproxy.MitmConnect
	// (que usa o CA built-in GoproxyCa) so que com a nossa CA.
	mitmAction := &goproxy.ConnectAction{
		Action:    goproxy.ConnectMitm,
		TLSConfig: goproxy.TLSConfigFromCA(&tlsCert),
	}

	gp := goproxy.NewProxyHttpServer()
	gp.Verbose = false
	// Upstream transport: por padrao (pentest) aceita cert quebrado.
	// SetStrictUpstream(true) troca para validacao estrita.
	p.mu.Lock()
	strict := p.strictUpstream
	p.mu.Unlock()
	gp.Tr = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !strict},
	}
	gp.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		return mitmAction, host
	})
	gp.OnRequest().DoFunc(p.onRequest)
	gp.OnResponse().DoFunc(p.onResponse)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: gp, ReadHeaderTimeout: 10 * time.Second}

	p.mu.Lock()
	p.ln = ln
	p.addr = ln.Addr().String()
	p.server = srv
	p.mu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.logger.Printf("proxy.Serve: %v", err)
		}
	}()
	return nil
}

// Stop fecha o listener. Idempotente.
func (p *Proxy) Stop() {
	p.mu.Lock()
	srv, ln := p.server, p.ln
	p.server, p.ln = nil, nil
	p.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	if ln != nil {
		_ = ln.Close()
	}
}

// captured e o buffer entre onRequest e onResponse via ProxyCtx.UserData.
type captured struct {
	method  string
	url     string
	headers map[string][]string
	body    []byte
	inScope bool
}

func (p *Proxy) onRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	p.mu.Lock()
	scp := p.scope
	p.mu.Unlock()

	cap := &captured{
		method:  req.Method,
		url:     req.URL.String(),
		headers: cloneHeaders(req.Header),
	}
	if scp != nil && scp.Matches(req.URL) {
		cap.inScope = true
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			cap.body = b
			req.Body = io.NopCloser(bytes.NewReader(b))
		}
	}
	ctx.UserData = cap
	return req, nil
}

func (p *Proxy) onResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	cap, _ := ctx.UserData.(*captured)
	if cap == nil {
		return resp
	}
	rec := &store.Request{
		Ts:         time.Now().UnixMilli(),
		Method:     cap.method,
		URL:        cap.url,
		ReqHeaders: cap.headers,
	}
	if cap.inScope {
		var body []byte
		if resp.Body != nil {
			b, _ := io.ReadAll(resp.Body)
			body = b
			resp.Body = io.NopCloser(bytes.NewReader(b))
		}
		rec.ReqBody = cap.body
		rec.Status = resp.StatusCode
		rec.RespHeaders = cloneHeaders(resp.Header)
		rec.RespBody = body
		rec.RespLen = len(body)
	}
	if _, err := p.store.Insert(rec); err != nil {
		p.logger.Printf("proxy.Insert: %v", err)
	}
	return resp
}

func cloneHeaders(h http.Header) map[string][]string {
	if h == nil {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, v := range h {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
