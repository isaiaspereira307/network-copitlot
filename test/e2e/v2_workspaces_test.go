// ponytail: leaf commands inlineados; Task 19 extrai internal/cli e reusa NewRootCmd
// ponytail: buildRoot chamado por invocacao (nao cacheado) porque pflag retem estado entre Execute
package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/config"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/store"
	"github.com/spf13/cobra"
)

func TestE2E_V2Workspaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load()
	cfg.WorkspacePath = filepath.Join(dir, "ws")
	cfg.Save()
	repo := projects.NewRepo(cfg.WorkspacePath)
	active := projects.NewActiveState(repo, cfg)
	alPath := filepath.Join(dir, "audit.log")
	al, _ := audit.New(alPath)
	t.Cleanup(func() { al.Close() })

	// 1. criar projeto via CLI
	out := runCmd(t, active, repo, al, "project", "create", "--name", "HackerOne-X", "--type", "bugbounty")
	if !strings.Contains(out, "HackerOne-X") {
		t.Fatalf("project create: %s", out)
	}

	// 2. usar projeto
	out = runCmd(t, active, repo, al, "project", "use", "HackerOne-X")
	if !strings.Contains(out, "ativo") {
		t.Fatalf("project use: %s", out)
	}

	// 3. adicionar alvo com confirm
	out = runCmd(t, active, repo, al, "target", "add", "--host", "api.empresa.com", "--confirm")
	if !strings.Contains(out, "api.empresa.com") {
		t.Fatalf("target add: %s", out)
	}

	// 4. alvo sem confirm: erro
	if err := runCmdErr(t, active, repo, al, "target", "add", "--host", "evil.com"); err == nil {
		t.Fatal("expected error without --confirm")
	}

	// 5. usar alvo
	out = runCmd(t, active, repo, al, "target", "use", "api.empresa.com")
	if !strings.Contains(out, "ativo") {
		t.Fatalf("target use: %s", out)
	}

	// 6. verificar persistencia
	cfg2, _ := config.Load()
	if cfg2.ActiveProject != "HackerOne-X" || cfg2.ActiveTarget != "api.empresa.com" {
		t.Errorf("config not persisted: %+v", cfg2)
	}

	// 7. verificar filesystem
	mustExist(t, filepath.Join(cfg.WorkspacePath, "HackerOne-X", "meta.yaml"))
	mustExist(t, filepath.Join(cfg.WorkspacePath, "HackerOne-X", "targets", "api.empresa.com", "meta.yaml"))
	mustExist(t, filepath.Join(cfg.WorkspacePath, "HackerOne-X", "targets", "api.empresa.com", "requests.db"))

	// 8. verificar audit log
	data, _ := os.ReadFile(alPath)
	if !strings.Contains(string(data), "create") {
		t.Errorf("audit log missing create: %s", data)
	}
}

func runCmd(t *testing.T, active *projects.ActiveState, repo *projects.Repo, al *audit.Logger, args ...string) string {
	t.Helper()
	root := buildRoot(active, repo, al)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("cmd %v: %v", args, err)
	}
	return buf.String()
}

func runCmdErr(t *testing.T, active *projects.ActiveState, repo *projects.Repo, al *audit.Logger, args ...string) error {
	t.Helper()
	root := buildRoot(active, repo, al)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	return root.Execute()
}

func mustExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err != nil {
		t.Errorf("expected %s: %v", p, err)
	}
}

// buildRoot replica minima de cmd/mcp-proxy/root.go: 4 leaf commands cobrem o fluxo E2E.
// Task 19 extrai internal/cli e este builder vira NewRootCmd reusado.
func buildRoot(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	root := &cobra.Command{Use: "mcp-proxy"}

	projectCreate := &cobra.Command{
		Use:   "create",
		Short: "Cria um novo projeto",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := &projects.Project{
				Name:     cmd.Flags().Lookup("name").Value.String(),
				Type:     projects.ProjectType(cmd.Flags().Lookup("type").Value.String()),
				Program:  cmd.Flags().Lookup("program").Value.String(),
				Platform: cmd.Flags().Lookup("platform").Value.String(),
			}
			if err := repo.CreateProject(p); err != nil {
				_ = al.Log(audit.Event{Tool: "project create", Action: "error", Detail: err.Error()})
				return err
			}
			_ = al.Log(audit.Event{Tool: "project create", Action: "create", Detail: map[string]any{"name": p.Name}})
			fmt.Fprintf(cmd.OutOrStdout(), "projeto criado: %s\n", p.Name)
			return nil
		},
	}
	projectCreate.Flags().String("name", "", "nome")
	projectCreate.Flags().String("type", "", "tipo")
	projectCreate.Flags().String("program", "", "programa")
	projectCreate.Flags().String("platform", "", "plataforma")
	_ = projectCreate.MarkFlagRequired("name")
	_ = projectCreate.MarkFlagRequired("type")

	projectUse := &cobra.Command{
		Use:  "use NAME",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := active.SetProject(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "projeto ativo: %s\n", args[0])
			return nil
		},
	}
	projectCmd := &cobra.Command{Use: "project"}
	projectCmd.AddCommand(projectCreate, projectUse)

	targetAdd := &cobra.Command{
		Use: "add",
		RunE: func(cmd *cobra.Command, args []string) error {
			host := cmd.Flags().Lookup("host").Value.String()
			if !cmd.Flags().Changed("confirm") {
				return fmt.Errorf("voce deve passar --confirm para confirmar que tem autorizacao para testar %q", host)
			}
			proj, err := active.Project()
			if err != nil || proj == nil {
				return fmt.Errorf("nenhum projeto ativo; use 'mcp-proxy project use NAME' primeiro")
			}
			tgt := &projects.Target{Host: host}
			if err := repo.AddTarget(proj.Name, tgt); err != nil {
				_ = al.Log(audit.Event{Tool: "target add", Action: "error", Detail: err.Error()})
				return err
			}
			// inicializa storage SQLite do alvo (cria requests.db vazio com schema)
			dbPath := filepath.Join(repo.WorkspacePath(), proj.Name, "targets", host, "requests.db")
			st, err := store.OpenSQLite(dbPath)
			if err != nil {
				return fmt.Errorf("abre store: %w", err)
			}
			_ = st.Close()
			_ = al.Log(audit.Event{Tool: "target add", Action: "add", Detail: map[string]any{"host": host, "project": proj.Name}})
			fmt.Fprintf(cmd.OutOrStdout(), "alvo adicionado: %s/%s\n", proj.Name, host)
			return nil
		},
	}
	targetAdd.Flags().String("host", "", "host")
	targetAdd.Flags().Bool("confirm", false, "confirma autorizacao")
	_ = targetAdd.MarkFlagRequired("host")

	targetUse := &cobra.Command{
		Use:  "use HOST",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := active.SetTarget(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "alvo ativo: %s\n", args[0])
			return nil
		},
	}
	targetCmd := &cobra.Command{Use: "target"}
	targetCmd.AddCommand(targetAdd, targetUse)

	root.AddCommand(projectCmd, targetCmd)
	return root
}
