package cli

import (
	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/spf13/cobra"
)

func NewRootCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	root := &cobra.Command{
		Use:   "mcp-proxy",
		Short: "MCP proxy com workspaces por projeto",
	}
	root.AddCommand(
		newProjectCmd(active, repo, al),
		newTargetCmd(active, repo, al),
	)
	return root
}
