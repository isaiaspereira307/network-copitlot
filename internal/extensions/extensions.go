// Package extensions implements v5.0 Extensions API (BApps-like). Gerencia uma
// allowlist de extensions por projeto e oferece hooks (on_request/on_response/
// on_finding). O carregamento de plugins Go compilados via stdlib `plugin`
// (arquivos .so) documentado em docs/extensions-api.md — neste build expomos o
// mecanismo de allowlist + os hook-tests com extensions "builtin" (puro-Go).
package extensions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// HookContext resume o que uma extension pode observar.
type HookContext struct {
	Project  string            `json:"project"`
	Target   string            `json:"target"`
	Type     string            `json:"type"` // on_request | on_response | on_finding
	URL      string            `json:"url,omitempty"`
	Method   string            `json:"method,omitempty"`
	Evidence map[string]any    `json:"evidence,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

// Extension e a superficie de hooks de um plugin.
type Extension interface {
	Name() string
	// OnFinding cria findings adicionais (via callback); retorna zero findings
	// para nenhuma acao.
	OnFinding(ctx HookContext, emit func(map[string]any)) error
}

// builtin registra extensions nativas (puro-Go, exemplo de superfície de hooks).
var builtin = map[string]Extension{
	"aws-key-secret": awsKeyExt{},
}

// awsKeyExt exemplo: detecta AWS access key no contexto de finding e emite um
// finding de alta severidade. Ilustra o hook on_finding.
type awsKeyExt struct{}

func (awsKeyExt) Name() string { return "aws-key-secret" }
func (awsKeyExt) OnFinding(ctx HookContext, emit func(map[string]any)) error {
	if ctx.Type != "on_finding" {
		return nil
	}
	if v, ok := ctx.Evidence["secret"].(string); ok && looksLikeAWSKey(v) {
		emit(map[string]any{
			"type": "secret", "severity": "crit", "url": ctx.URL,
			"evidence": map[string]any{"aws_access_key": redactKey(v)},
		})
	}
	return nil
}

func looksLikeAWSKey(s string) bool {
	if len(s) != 20 {
		return false
	}
	const prefix = "AKIA"
	for i := 0; i < len(prefix); i++ {
		if s[i] != prefix[i] {
			return false
		}
	}
	return true
}

func redactKey(k string) string {
	if len(k) <= 8 {
		return "***"
	}
	return k[:4] + "***" + k[len(k)-3:]
}

// Manager gerencia a allowlist de extensions por projeto (persistida em JSON
// no workspace do projeto).
type Manager struct {
	mu       sync.Mutex
	workspace string
	enabled  map[string]map[string]bool // project -> extName -> enabled
}

// New cria um Manager apontando para o workspace (raiz de projetos).
func New(workspace string) *Manager {
	m := &Manager{workspace: workspace}
	m.reload()
	return m
}

func (m *Manager) path(project string) string {
	return filepath.Join(m.workspace, project, "extensions.json")
}

func (m *Manager) reload() {
	m.enabled = map[string]map[string]bool{}
	if m.workspace == "" {
		return
	}
	// lazy: carrega sob demanda em List/Enable
}

func (m *Manager) load(project string) map[string]bool {
	set := map[string]bool{}
	b, err := os.ReadFile(m.path(project))
	if err != nil {
		return set
	}
	_ = json.Unmarshal(b, &set)
	return set
}

func (m *Manager) save(project string, set map[string]bool) error {
	b, _ := json.Marshal(set)
	if m.workspace == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.path(project)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.path(project), b, 0o600)
}

// List devolve as extensions conhecidas (builtin) com status enabled por projeto.
func (m *Manager) List(project string) []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(builtin))
	for n := range builtin {
		names = append(names, n)
	}
	sort.Strings(names)
	en := m.load(project)
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{"name": n, "enabled": en[n], "builtin": true})
	}
	return out
}

// Enable ativa uma extension no projeto (allowlist).
func (m *Manager) Enable(project, ext string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := builtin[ext]; !ok {
		return ErrUnknown
	}
	en := m.load(project)
	en[ext] = true
	return m.save(project, en)
}

// Disable desativa uma extension no projeto.
func (m *Manager) Disable(project, ext string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := builtin[ext]; !ok {
		return ErrUnknown
	}
	en := m.load(project)
	delete(en, ext)
	return m.save(project, en)
}

// EnabledExtensions devolve as extensions ativas num projeto.
func (m *Manager) EnabledExtensions(project string) []Extension {
	m.mu.Lock()
	defer m.mu.Unlock()
	en := m.load(project)
	var out []Extension
	for n, on := range en {
		if on {
			if e, ok := builtin[n]; ok {
				out = append(out, e)
			}
		}
	}
	return out
}

// ErrUnknown indica extension inexistente.
var ErrUnknown = errUnknown("extension desconhecida")

type errUnknown string

func (e errUnknown) Error() string { return string(e) }
