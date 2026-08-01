package main

import (
	"fmt"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/spf13/cobra"
)

func newProjectCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Gerencia projetos (engajamentos)",
	}
	cmd.AddCommand(
		newProjectCreateCmd(active, repo, al),
		newProjectListCmd(active, repo, al),
		newProjectUseCmd(active, repo, al),
	)
	return cmd
}

func newProjectCreateCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	var (
		name     string
		typ      string
		program  string
		platform string
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Cria um novo projeto",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := &projects.Project{
				Name:     name,
				Type:     projects.ProjectType(typ),
				Program:  program,
				Platform: platform,
			}
			if err := repo.CreateProject(p); err != nil {
				al.Log(audit.Event{Tool: "project create", Action: "error", Detail: err.Error()})
				return err
			}
			al.Log(audit.Event{Tool: "project create", Action: "create", Detail: map[string]any{"name": name}})
			fmt.Fprintf(cmd.OutOrStdout(), "projeto criado: %s\n", name)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "nome do projeto (obrigatorio)")
	c.Flags().StringVar(&typ, "type", "", "tipo: bugbounty|pentest (obrigatorio)")
	c.Flags().StringVar(&program, "program", "", "nome do programa")
	c.Flags().StringVar(&platform, "platform", "", "plataforma (hackerone, bugcrowd, ...)")
	_ = c.MarkFlagRequired("name")
	_ = c.MarkFlagRequired("type")
	return c
}

func newProjectListCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista projetos",
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := repo.ListProjects()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nenhum projeto")
				return nil
			}
			for _, p := range list {
				marker := "  "
				_ = active
				fmt.Fprintf(cmd.OutOrStdout(), "%s%s\t%s\n", marker, p.Name, p.Type)
			}
			return nil
		},
	}
}

func newProjectUseCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "use NAME",
		Short: "Define projeto ativo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := active.SetProject(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "projeto ativo: %s\n", args[0])
			return nil
		},
	}
}
