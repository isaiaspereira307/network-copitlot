package mcpserver

import (
	"context"
	"path/filepath"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go SDK e expoe as 7 tools v2 via stdio.
type Server struct {
	active      *projects.ActiveState
	repo        *projects.Repo
	audit       *audit.Logger
	mcp         *mcpsdk.MCPServer
	currentStore store.Store
}

func New(active *projects.ActiveState, repo *projects.Repo, a *audit.Logger) *Server {
	m := mcpsdk.NewMCPServer("mcp-proxy", "v0.2.0")
	s := &Server{active: active, repo: repo, audit: a, mcp: m}
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
