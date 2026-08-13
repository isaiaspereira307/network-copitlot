package cli

import (
	"fmt"
	"path/filepath"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
	"github.com/spf13/cobra"
)

func newTargetCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "target",
		Short: "Gerencia alvos dentro do projeto ativo",
	}
	cmd.AddCommand(
		newTargetAddCmd(active, repo, al),
		newTargetListCmd(active, repo),
		newTargetUseCmd(active),
	)
	return cmd
}

func newTargetAddCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	var (
		host     string
		confirm  bool
		inScope  []string
		outScope []string
		notes    string
	)
	c := &cobra.Command{
		Use:   "add",
		Short: "Adiciona um alvo ao projeto ativo (exige --confirm)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("voce deve passar --confirm para confirmar que tem autorizacao para testar %q", host)
			}
			proj, err := active.Project()
			if err != nil || proj == nil {
				return fmt.Errorf("nenhum projeto ativo; use 'mcp-proxy project use NAME' primeiro")
			}
			tgt := &projects.Target{
				Host:               host,
				InScopePatterns:    inScope,
				OutOfScopePatterns: outScope,
				Notes:              notes,
			}
			if err := repo.AddTarget(proj.Name, tgt); err != nil {
				_ = al.Log(audit.Event{Tool: "target add", Action: "error", Detail: err.Error()})
				return err
			}
			dbPath := filepath.Join(tgt.Dir(proj.Dir(repo.WorkspacePath())), "requests.db")
			if _, err := store.OpenSQLite(dbPath); err != nil {
				_ = al.Log(audit.Event{Tool: "target add", Action: "error", Detail: map[string]any{"err": err.Error()}})
				return fmt.Errorf("abrir store: %w", err)
			}
			_ = al.Log(audit.Event{Tool: "target add", Action: "add", Detail: map[string]any{"host": host, "project": proj.Name}})
			fmt.Fprintf(cmd.OutOrStdout(), "alvo adicionado: %s/%s\n", proj.Name, host)
			return nil
		},
	}
	c.Flags().StringVar(&host, "host", "", "host do alvo (obrigatorio)")
	c.Flags().BoolVar(&confirm, "confirm", false, "confirma que voce tem autorizacao para testar este alvo")
	c.Flags().StringSliceVar(&inScope, "in-scope", nil, "padroes in-scope (CSV)")
	c.Flags().StringSliceVar(&outScope, "out-of-scope", nil, "padroes out-of-scope (CSV)")
	c.Flags().StringVar(&notes, "notes", "", "notas livres")
	_ = c.MarkFlagRequired("host")
	return c
}

func newTargetListCmd(active *projects.ActiveState, repo *projects.Repo) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista alvos do projeto ativo",
		RunE: func(cmd *cobra.Command, args []string) error {
			proj, err := active.Project()
			if err != nil || proj == nil {
				return fmt.Errorf("nenhum projeto ativo")
			}
			list, err := repo.ListTargets(proj.Name)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nenhum alvo")
				return nil
			}
			for _, t := range list {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", t.Host)
			}
			return nil
		},
	}
}

func newTargetUseCmd(active *projects.ActiveState) *cobra.Command {
	return &cobra.Command{
		Use:   "use HOST",
		Short: "Define alvo ativo dentro do projeto ativo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := active.SetTarget(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "alvo ativo: %s\n", args[0])
			return nil
		},
	}
}
