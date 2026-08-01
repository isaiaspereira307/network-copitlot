package main

import (
	"context"
	"fmt"
	"os"

	"github.com/isaias/network-copitlot/internal/audit"
	"github.com/isaias/network-copitlot/internal/config"
	"github.com/isaias/network-copitlot/internal/mcpserver"
	"github.com/isaias/network-copitlot/internal/projects"
	"github.com/isaias/network-copitlot/internal/store"
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

	if len(os.Args) < 2 {
		return runMCPServer(active, repo, al)
	}
	cmd := NewRootCmd(active, repo, al)
	return cmd.Execute()
}

func runMCPServer(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) error {
	s := mcpserver.New(active, repo, al)
	s.RegisterTools()
	return s.Run(context.Background())
}
