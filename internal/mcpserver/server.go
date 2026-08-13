package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go SDK e expoe as 7 tools v2 via stdio.
type Server struct {
	active       *projects.ActiveState
	repo         *projects.Repo
	audit        *audit.Logger
	mcp          *mcpsdk.MCPServer
	currentStore store.Store
	mu           sync.Mutex // guards currentStore
	tools        map[string]toolFunc
}

func New(active *projects.ActiveState, repo *projects.Repo, a *audit.Logger) *Server {
	m := mcpsdk.NewMCPServer("mcp-proxy", "v0.2.0")
	s := &Server{active: active, repo: repo, audit: a, mcp: m, tools: map[string]toolFunc{}}
	s.RegisterTools()
	return s
}

// RegisterTools registra as 7 tools v2 no server mcp-go. Idempotente.
func (s *Server) RegisterTools() {
	registerV2Tools(s)
	if s.mcp.GetTool("create_project") != nil {
		return // ja registradas
	}
	s.mcp.AddTool(
		mcp.NewTool("create_project",
			mcp.WithDescription("Cria um novo projeto (engajamento) de bug bounty ou pentest"),
			mcp.WithString("name", mcp.Required()),
			mcp.WithString("type", mcp.Required(), mcp.Enum("bugbounty", "pentest")),
			mcp.WithString("program"),
			mcp.WithString("platform"),
		),
		s.wrapTool("create_project", s.toolCreateProject),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_projects",
			mcp.WithDescription("Lista todos os projetos"),
		),
		s.wrapTool("list_projects", s.toolListProjects),
	)
	s.mcp.AddTool(
		mcp.NewTool("set_active_project",
			mcp.WithDescription("Define o projeto ativo"),
			mcp.WithString("name", mcp.Required()),
		),
		s.wrapTool("set_active_project", s.toolSetActiveProject),
	)
	s.mcp.AddTool(
		mcp.NewTool("add_target",
			mcp.WithDescription("Adiciona alvo ao projeto ativo. Cliente MCP deve passar confirmed=true (PRD §5)"),
			mcp.WithString("host", mcp.Required()),
			mcp.WithBoolean("confirmed", mcp.Required()),
			mcp.WithArray("in_scope", mcp.WithStringItems()),
			mcp.WithArray("out_of_scope", mcp.WithStringItems()),
		),
		s.wrapTool("add_target", s.toolAddTarget),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_targets",
			mcp.WithDescription("Lista alvos do projeto ativo"),
		),
		s.wrapTool("list_targets", s.toolListTargets),
	)
	s.mcp.AddTool(
		mcp.NewTool("set_active_target",
			mcp.WithDescription("Define o alvo ativo no projeto ativo"),
			mcp.WithString("host", mcp.Required()),
		),
		s.wrapTool("set_active_target", s.toolSetActiveTarget),
	)
	s.mcp.AddTool(
		mcp.NewTool("get_active_context",
			mcp.WithDescription("Retorna projeto/alvo ativos e contagem de requests"),
		),
		s.wrapTool("get_active_context", s.toolGetActiveContext),
	)
	s.mcp.AddTool(
		mcp.NewTool("list_requests",
			mcp.WithDescription("List captured requests paginated by recency. Returns ONLY summary fields (id, ts, method, url, status, resp_len) — never bodies. Use filters to narrow; default limit=50. Page with offset/since_id. Pick interesting ids then call get_request_detail."),
			mcp.WithString("method_filter"),
			mcp.WithNumber("status_filter"),
			mcp.WithString("host_filter"),
			mcp.WithString("path_contains"),
			mcp.WithNumber("limit"),
			mcp.WithNumber("offset"),
			mcp.WithNumber("since_id"),
		),
		s.wrapTool("list_requests", s.toolListRequests),
	)
	s.mcp.AddTool(
		mcp.NewTool("get_request_detail",
			mcp.WithDescription("Get full detail of one captured request (id from list_requests). include: headers (default, cheapest), body, or all. Bodies are truncated to max_body_bytes (default 8192) — check body_truncated/total_len; use body_range (e.g. '0-4096') to page through large bodies. Never request full bodies of large assets; use search_bodies or summarize first."),
			mcp.WithNumber("id", mcp.Required()),
			mcp.WithString("include", mcp.Enum("headers", "body", "all")),
			mcp.WithNumber("max_body_bytes"),
			mcp.WithString("body_range"),
		),
		s.wrapTool("get_request_detail", s.toolGetRequestDetail),
	)
	s.mcp.AddTool(
		mcp.NewTool("search_bodies",
			mcp.WithDescription("Search request and response bodies of the active target for a pattern: regex if the query compiles as one, otherwise a case-sensitive substring. Returns short ±80-char snippets with request ids and urls — never full bodies. Scope to request bodies (req), response bodies (resp), or both (default). Follow up interesting hits with get_request_detail for the full request."),
			mcp.WithString("query", mcp.Required()),
			mcp.WithString("scope", mcp.Enum("req", "resp", "both")),
			mcp.WithNumber("limit"),
		),
		s.wrapTool("search_bodies", s.toolSearchBodies),
	)
	s.mcp.AddTool(
		mcp.NewTool("replay_request",
			mcp.WithDescription("Re-executes a captured request (id from list_requests) against its target host, guarded by the active target's scope. Optional overrides: url (new target URL), method, headers (string map to set), body (string), follow_redirects (default false — a 3xx is returned as-is; when true, each redirect is re-checked against scope). The replayed exchange is persisted as a NEW request; the result returns only new_request_id, status, resp_len — never bodies. Out-of-scope hosts are blocked with a clear error."),
			mcp.WithNumber("id", mcp.Required()),
			mcp.WithString("url"),
			mcp.WithString("method"),
			mcp.WithObject("headers", mcp.WithStringItems()),
			mcp.WithString("body"),
			mcp.WithBoolean("follow_redirects"),
		),
		s.wrapTool("replay_request", s.toolReplayRequest),
	)
	s.mcp.AddTool(
		mcp.NewTool("set_scope",
			mcp.WithDescription("Persist new in-scope patterns for the ACTIVE target. Writes in_scope to the target's meta.yaml on disk, overwriting previous in-scope patterns; existing out-of-scope patterns are preserved. The live proxy picks up the change automatically on its next request via a cheap mtime check of the meta.yaml — no proxy restart required. Pass an empty array to clear all in-scope restrictions (anything not explicitly out-of-scope becomes allowed)."),
			mcp.WithArray("in_scope", mcp.Required(), mcp.WithStringItems()),
		),
		s.wrapTool("set_scope", s.toolSetScope),
	)
}

// wrapTool adapta toolFunc (ctx, map[string]any) para ToolHandlerFunc do mcp-go.
func (s *Server) wrapTool(name string, fn toolFunc) mcpsdk.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := req.Params.Arguments.(map[string]any)
		out, err := fn(ctx, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

// openStoreForActiveTarget abre (ou recria) o store SQLite do alvo ativo.
// Retorna nil se nao ha alvo ativo. Fecha o anterior se existir.
func (s *Server) openStoreForActiveTarget() (store.Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentStore != nil {
		_ = s.currentStore.Close()
		s.currentStore = nil
	}
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return nil, nil
	}
	tgt, err := s.active.Target()
	if err != nil || tgt == nil {
		return nil, nil
	}
	targetDir := tgt.Dir(proj.Dir(s.repo.WorkspacePath()))
	dbPath := filepath.Join(targetDir, "requests.db")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		return nil, err
	}
	s.currentStore = st
	return st, nil
}

// Run inicia o servidor MCP em stdio. Bloqueia ate EOF no stdin.
// ponytail: ctx nao honrado — ServeStdio nao aceita ctx. Wire com goroutine
// + cancelacao manual quando o SDK expor. Manter param evita quebra de API.
func (s *Server) Run(ctx context.Context) error {
	_ = ctx
	return mcpsdk.ServeStdio(s.mcp)
}

// CallTool invoca uma tool registrada fora do protocolo stdio: harness e2e e
// clientes programaticos. Mesma via que o SDK usa internamente (s.tools).
func (s *Server) CallTool(name string, args map[string]any) (string, error) {
	fn, ok := s.tools[name]
	if !ok {
		return "", fmt.Errorf("tool %s nao registrada", name)
	}
	return fn(context.Background(), args)
}
