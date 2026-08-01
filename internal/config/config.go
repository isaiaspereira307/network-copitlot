package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	WorkspacePath string `yaml:"workspace_path"`
	ActiveProject string `yaml:"active_project"`
	ActiveTarget  string `yaml:"active_target"`
}

const dirName = ".mcp-proxy"
const fileName = "config.yaml"

// Path retorna o path absoluto de ~/.mcp-proxy/config.yaml.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName, fileName), nil
}

// EnsureDir cria ~/.mcp-proxy se nao existir. Idempotente.
func EnsureDir() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(home, dirName), 0o755)
}

// Default retorna Config com WorkspacePath = ~/.mcp-proxy/workspace.
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		WorkspacePath: filepath.Join(home, dirName, "workspace"),
	}
}

// Load le config.yaml; se nao existir retorna Default sem erro.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	} else if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Save cria diretorio ~/.mcp-proxy se preciso e grava YAML.
func (c *Config) Save() error {
	if err := EnsureDir(); err != nil {
		return err
	}
	p, err := Path()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}
