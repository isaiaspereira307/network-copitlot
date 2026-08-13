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
	Proxy         Proxy  `yaml:"proxy"`
}

// Proxy define os limites de captura de body do proxy MITM (task 17):
// globs de Content-Type que pulam a captura e cap de bytes do body capturado.
type Proxy struct {
	NoBodyContentTypes []string `yaml:"no_body_content_types"`
	BodyCapBytes       int64    `yaml:"body_cap_bytes"`
}

// DefaultProxy retorna os defaults de captura: pula content-types binarios/
// estaticos e cap de 1MB.
func DefaultProxy() Proxy {
	return Proxy{
		NoBodyContentTypes: []string{"image/*", "font/*", "video/*", "text/css"},
		BodyCapBytes:       1 << 20,
	}
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
		Proxy:         DefaultProxy(),
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
