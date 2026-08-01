package mcpserver

import (
	"context"
	"path/filepath"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/store"
	mcpsdk "github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go SDK and holds the dependencies the future tools
// (tasks 12-14) will need. No tools registered yet.
type Server struct {
	active      *projects.ActiveState
	repo        *projects.Repo
	audit       *audit.Logger
	mcp         *mcpsdk.MCPServer
	currentStore store.Store
}

func New(active *projects.ActiveState, repo *projects.Repo, a *audit.Logger) *Server {
	m := mcpsdk.NewMCPServer("mcp-proxy", "v0.2.0")
	return &Server{active: active, repo: repo, audit: a, mcp: m}
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
