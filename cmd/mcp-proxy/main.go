package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/cli"
	"github.com/isaias/network-copitlot/internal/config"
	"github.com/isaias/network-copitlot/internal/mcpserver"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/proxy"
	"github.com/isaias/network-copitlot/internal/store"
	"github.com/spf13/cobra"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if err := store.MigrateV1ToV2(cfg.WorkspacePath); err != nil {
		return err
	}
	repo := projects.NewRepo(cfg.WorkspacePath)
	active := projects.NewActiveState(repo, cfg)
	alPath, _ := audit.DefaultPath()
	al, err := audit.New(alPath)
	if err != nil {
		return err
	}
	defer al.Close()

	root := cli.NewRootCmd(active, repo, al)
	root.AddCommand(newProxyCmd(active, repo, al))

	if len(os.Args) < 2 {
		return runMCPServer(active, repo, al)
	}
	return root.Execute()
}

func runMCPServer(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) error {
	s := mcpserver.New(active, repo, al)
	s.RegisterTools()
	return s.Run(context.Background())
}

// newProxyCmd: `mcp-proxy proxy [--addr :8080]` sobe o MITM standalone.
// Requer projeto + alvo ativo; grava em
// <workspace>/<projeto>/targets/<host>/requests.db.
func newProxyCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	var (
		addr               string
		bodyCap            int64
		noBodyContentTypes string
	)
	c := &cobra.Command{
		Use:   "proxy",
		Short: "Sobe o proxy MITM HTTP/HTTPS standalone (default :8080)",
		Long: `Sobe o proxy MITM HTTP/HTTPS no endereco --addr. Requer projeto+alvo
ativo. Conexoes in-scope sao gravadas com body; out-of-scope sao gravadas
sem body (apenas metadados, conforme PRD §4.1). O CA esta em
~/.mcp-proxy/ca/cert.pem — instale no browser/emulador para MITM HTTPS.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			proj, err := active.Project()
			if err != nil || proj == nil {
				return fmt.Errorf("nenhum projeto ativo; use 'mcp-proxy project use NAME' primeiro")
			}
			tgt, err := active.Target()
			if err != nil || tgt == nil {
				return fmt.Errorf("nenhum alvo ativo; use 'mcp-proxy target use HOST' primeiro")
			}
			dbPath := filepath.Join(tgt.Dir(proj.Dir(repo.WorkspacePath())), "requests.db")
			st, err := store.OpenSQLite(dbPath)
			if err != nil {
				return fmt.Errorf("abrir store: %w", err)
			}
			defer st.Close()
			caDir, _ := defaultCADir()
			// captura de body (task 17): defaults de config.yaml; flags explicitas
			// sobrescrevem.
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("carregar config: %w", err)
			}
			pcfg := cfg.Proxy
			if cmd.Flags().Changed("body-cap") {
				pcfg.BodyCapBytes = bodyCap
			}
			if cmd.Flags().Changed("no-body-content-types") {
				pcfg.NoBodyContentTypes = nil
				for _, g := range strings.Split(noBodyContentTypes, ",") {
					if g = strings.TrimSpace(g); g != "" {
						pcfg.NoBodyContentTypes = append(pcfg.NoBodyContentTypes, g)
					}
				}
			}
			p := proxy.NewProxy(st, caDir)
			p.SetCaptureConfig(pcfg)
			// recarga viva: MCP server (processo separado) persiste o scope no
			// meta.yaml; o proxy observa o mtime e rele do disco a cada request.
			metaPath := filepath.Join(tgt.Dir(proj.Dir(repo.WorkspacePath())), "meta.yaml")
			p.SetTargetReload(tgt, metaPath, func() (*projects.Target, error) {
				return repo.LoadTarget(proj.Name, tgt.Host)
			})
			if err := p.Start(addr); err != nil {
				return err
			}
			_ = al.Log(audit.Event{
				Tool:   "proxy",
				Action: "start",
				Detail: map[string]any{"addr": p.Addr(), "host": tgt.Host},
			})
			fmt.Fprintf(cmd.OutOrStdout(),
				"proxy escutando em %s (alvo: %s/%s)\n", p.Addr(), proj.Name, tgt.Host)
			fmt.Fprintf(cmd.OutOrStdout(),
				"CA: %s/cert.pem — instale no browser/emulador para MITM HTTPS\n", caDir)
			fmt.Fprintln(cmd.OutOrStdout(), "Ctrl+C para parar")

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			<-sig
			p.Stop()
			_ = al.Log(audit.Event{Tool: "proxy", Action: "stop"})
			return nil
		},
	}
	c.Flags().StringVar(&addr, "addr", ":8080", "endereco de escuta do proxy")
	c.Flags().Int64Var(&bodyCap, "body-cap", 0, "cap de bytes do body capturado (0 = default 1048576)")
	c.Flags().StringVar(&noBodyContentTypes, "no-body-content-types", "", "globs de Content-Type a pular na captura, separados por virgula (default image/*,font/*,video/*,text/css)")
	return c
}

// defaultCADir retorna ~/.mcp-proxy/ca.
func defaultCADir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mcp-proxy", "ca"), nil
}
