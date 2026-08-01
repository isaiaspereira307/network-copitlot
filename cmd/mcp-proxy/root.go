package main

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

// newTargetCmd é um stub; a implementação completa vem na Task 17.
func newTargetCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "target",
		Short: "Gerencia alvos do projeto ativo (stub — Task 17)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
}
