package mcpserver

import (
	"context"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/projects"
	mcpsdk "github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go SDK and holds the dependencies the future tools
// (tasks 12-14) will need. No tools registered yet.
type Server struct {
	active *projects.ActiveState
	repo   *projects.Repo
	audit  *audit.Logger
	mcp    *mcpsdk.MCPServer
}

func New(active *projects.ActiveState, repo *projects.Repo, a *audit.Logger) *Server {
	m := mcpsdk.NewMCPServer("mcp-proxy", "v0.2.0")
	return &Server{active: active, repo: repo, audit: a, mcp: m}
}

// Run inicia o servidor MCP em stdio. Bloqueia ate EOF no stdin.
// ponytail: ctx nao honrado — ServeStdio nao aceita ctx. Wire com goroutine
// + cancelacao manual quando o SDK expor. Manter param evita quebra de API.
func (s *Server) Run(ctx context.Context) error {
	_ = ctx
	return mcpsdk.ServeStdio(s.mcp)
}
