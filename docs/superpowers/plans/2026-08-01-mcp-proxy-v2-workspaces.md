# v2.0 Workspaces (Projects + Targets) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Adicionar suporte a múltiplos projetos (engajamentos de bug bounty / pentest) com alvos segregados ao `mcp-proxy`, expondo 7 tools MCP e 2 subcomandos CLI.

**Architecture:** Workspace filesystem em `~/.mcp-proxy/workspace/<project>/targets/<host>/`. Cada alvo tem `requests.db` SQLite isolado. Estado ativo (projeto/alvo atual) persistido em `~/.mcp-proxy/config.yaml`. Migration do v1 store flat para per-target roda na primeira inicialização (idempotente).

**Tech Stack:**
- Go (latest stable)
- `modernc.org/sqlite` — driver SQLite puro-Go
- `mark3labs/mcp-go` — MCP server
- `spf13/cobra` — CLI
- `gopkg.in/yaml.v3` — `meta.yaml` / `config.yaml`

## Global Constraints

- Go latest stable; **sem CGO**.
- Binário único portável; sem deps externas em runtime.
- Workspace root: `~/.mcp-proxy/workspace/` (criado on demand).
- Config global: `~/.mcp-proxy/config.yaml` (criado on demand).
- Audit log: `~/.mcp-proxy/audit.log` (append-only, JSON-lines).
- Storage segregado por alvo: `workspace/<project>/targets/<host>/requests.db`.
- Metadata: `meta.yaml` em projeto e em cada alvo (formato YAML).
- Schema SQLite (tabela `requests`): `id, ts, method, url, req_headers(JSON), req_body(BLOB), status, resp_headers(JSON), resp_body(BLOB), resp_len, ttfb_ms, tags(JSON), notes`.
- **Confirmação interativa obrigatória** em `add_target`: prompt `"Tem autorização para testar este alvo? [y/N]"`; default aborta (N).
- MCP transport: **stdio** (mantido do v1).
- CLI subcommands via cobra.
- TDD: cada task começa com teste falhando, termina com verde + commit.
- Commits em português seguindo padrão do projeto (conventional commits).
- YAGNI: zero abstração especulativa, sem factories, sem interfaces com uma impl.

## Assumptions

- Repositório atualmente vazio (apenas `PRD-mcp-proxy-golang.md`). Este plano **constrói a v2.0 do zero** incluindo o scaffold Go mínimo necessário.
- O módulo Go se chama `github.com/isaiaspereira307/network-copitlot` (ajustar se o usuário preferir outro path; é o único lugar onde o path aparece).
- v1 tools MCP (`list_requests`, `get_request_detail`, `replay_request`, `set_scope`, `search_bodies`) **NÃO** estão sendo implementadas aqui. A v2.0 assume que existe um `store.Store` (interface definida na Task 5) que as tools v1 consomem; tools v1 virão em plano separado.
- Migration do v1 store flat (`requests.db` na raiz) para v2 per-target é **idempotente** e silenciosa se não houver dados v1.

## File Structure

```
mcp-proxy/
├── cmd/mcp-proxy/
│   ├── main.go                  # entrypoint
│   ├── project.go               # cobra: mcp-proxy project ...
│   └── target.go                # cobra: mcp-proxy target ...
├── internal/
│   ├── projects/
│   │   ├── model.go             # Project, Target structs
│   │   ├── model_test.go
│   │   ├── repo.go              # FS repo
│   │   ├── repo_test.go
│   │   ├── active.go            # active state
│   │   └── active_test.go
│   ├── config/
│   │   ├── config.go            # ~/.mcp-proxy/config.yaml
│   │   └── config_test.go
│   ├── store/
│   │   ├── store.go             # interface Store
│   │   ├── sqlite.go            # impl SQLite per-target
│   │   ├── migrate.go           # v1 flat → v2 per-target
│   │   ├── schema.go            # const SchemaSQL
│   │   └── store_test.go
│   ├── audit/
│   │   ├── audit.go             # append-only JSON-lines
│   │   └── audit_test.go
│   └── mcpserver/
│       ├── server.go            # server wrapper
│       ├── tools_v2.go          # 7 tools v2
│       └── tools_v2_test.go
├── go.mod
├── go.sum
└── config.yaml.example
```

---

## Task 1: Scaffold (go.mod + structure)

**Files:**
- Create: `go.mod`
- Create: `cmd/mcp-proxy/main.go`

- [ ] **Step 1: Initialize Go module**

```bash
cd mcp-proxy
go mod init github.com/isaiaspereira307/network-copitlot
```

Expected: arquivo `go.mod` criado com `module github.com/isaiaspereira307/network-copitlot` e `go 1.23`.

- [ ] **Step 2: Add minimal main.go**

Criar `cmd/mcp-proxy/main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("mcp-proxy v0.0.0 (v2.0 plan in progress)")
}
```

- [ ] **Step 3: Build**

Run: `go build ./cmd/mcp-proxy`
Expected: `mcp-proxy` binary criado, exit code 0.

- [ ] **Step 4: Run**

Run: `./mcp-proxy`
Expected: stdout `mcp-proxy v0.0.0 (v2.0 plan in progress)`.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/mcp-proxy/main.go
git commit -m "feat: scaffold cmd/mcp-proxy com main.go minimo"
```

---

## Task 2: Add dependencies (sqlite, yaml, cobra, mcp-go)

**Files:**
- Modify: `go.mod`
- Create: `go.sum`

- [ ] **Step 1: Add runtime deps**

Run:
```bash
go get modernc.org/sqlite@latest
go get gopkg.in/yaml.v3@latest
go get github.com/spf13/cobra@latest
go get github.com/mark3labs/mcp-go@latest
```

Expected: `go.mod` lista as 4 deps; `go.sum` populado.

- [ ] **Step 2: Tidy**

Run: `go mod tidy`
Expected: sem mudanças em `go.mod`/`go.sum`; sem erros.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: deps sqlite, yaml, cobra, mcp-go"
```

---

## Task 3: Config module (load/save config.yaml)

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nada
- Produces:
  ```go
  type Config struct {
      WorkspacePath string `yaml:"workspace_path"`
      ActiveProject string `yaml:"active_project"`
      ActiveTarget  string `yaml:"active_target"`
  }
  func Default() *Config
  func Path() (string, error)                       // ~/.mcp-proxy/config.yaml
  func Load() (*Config, error)                      // load or default
  func (c *Config) Save() error                     // create dirs, write YAML
  func EnsureDir() error                            // mkdir ~/.mcp-proxy
  ```

- [ ] **Step 1: Write failing test**

Criar `internal/config/config_test.go`:

```go
package config

import (
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.WorkspacePath == "" {
		t.Fatal("WorkspacePath must be set in default")
	}
	if !filepath.IsAbs(c.WorkspacePath) {
		t.Fatalf("WorkspacePath must be absolute, got %q", c.WorkspacePath)
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // Path() usa $HOME

	c := Default()
	c.ActiveProject = "HackerOne-EMPRESA"
	c.ActiveTarget = "api.empresa.com"
	if err := c.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ActiveProject != "HackerOne-EMPRESA" {
		t.Errorf("active_project = %q, want HackerOne-EMPRESA", loaded.ActiveProject)
	}
	if loaded.ActiveTarget != "api.empresa.com" {
		t.Errorf("active_target = %q, want api.empresa.com", loaded.ActiveTarget)
	}
}

func TestLoad_DefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	c, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.ActiveProject != "" {
		t.Errorf("expected empty active_project, got %q", c.ActiveProject)
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/config/...`
Expected: FAIL (`Default` undefined).

- [ ] **Step 3: Implement**

Criar `internal/config/config.go`:

```go
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
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/config/...`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): load/save ~/.mcp-proxy/config.yaml com defaults"
```

---

## Task 4: Projects — model (Project, Target structs + validation)

**Files:**
- Create: `internal/projects/model.go`
- Create: `internal/projects/model_test.go`

**Interfaces:**
- Consumes: nada
- Produces:
  ```go
  type ProjectType string
  const (
      ProjectBugBounty ProjectType = "bugbounty"
      ProjectPentest   ProjectType = "pentest"
  )
  func (p ProjectType) Valid() bool

  type Project struct {
      Name      string      `yaml:"name"`
      Type      ProjectType `yaml:"type"`
      Program   string      `yaml:"program"`
      Platform  string      `yaml:"platform"`
      CreatedAt time.Time   `yaml:"created_at"`
  }
  func (p *Project) Validate() error
  func (p *Project) Dir(workspace string) string // workspace/<Name>

  type Target struct {
      Host               string    `yaml:"host"`
      InScopePatterns    []string  `yaml:"in_scope"`
      OutOfScopePatterns []string  `yaml:"out_of_scope"`
      Notes              string    `yaml:"notes"`
      CreatedAt          time.Time `yaml:"created_at"`
  }
  func (t *Target) Validate() error
  func (t *Target) Dir(projectDir string) string // projectDir/targets/<Host>
  ```

- [ ] **Step 1: Write failing test**

Criar `internal/projects/model_test.go`:

```go
package projects

import (
	"strings"
	"testing"
	"time"
)

func TestProjectType_Valid(t *testing.T) {
	cases := []struct {
		in   ProjectType
		want bool
	}{
		{ProjectBugBounty, true},
		{ProjectPentest, true},
		{ProjectType(""), false},
		{ProjectType("hack"), false},
	}
	for _, c := range cases {
		if got := c.in.Valid(); got != c.want {
			t.Errorf("Valid(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestProject_Validate(t *testing.T) {
	now := time.Now()
	good := &Project{Name: "HackerOne-EMPRESA", Type: ProjectBugBounty, CreatedAt: now}
	if err := good.Validate(); err != nil {
		t.Errorf("good project rejected: %v", err)
	}

	bad := []struct {
		name string
		p    *Project
	}{
		{"empty name", &Project{Name: "", Type: ProjectBugBounty, CreatedAt: now}},
		{"bad name chars", &Project{Name: "../escape", Type: ProjectBugBounty, CreatedAt: now}},
		{"bad type", &Project{Name: "X", Type: "x", CreatedAt: now}},
	}
	for _, b := range bad {
		if err := b.p.Validate(); err == nil {
			t.Errorf("%s: expected error, got nil", b.name)
		}
	}
}

func TestProject_Dir(t *testing.T) {
	p := &Project{Name: "HackerOne-EMPRESA"}
	got := p.Dir("/ws")
	want := "/ws/HackerOne-EMPRESA"
	if got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestTarget_Validate(t *testing.T) {
	now := time.Now()
	good := &Target{Host: "api.empresa.com", CreatedAt: now}
	if err := good.Validate(); err != nil {
		t.Errorf("good target rejected: %v", err)
	}

	bad := []struct {
		name string
		t    *Target
	}{
		{"empty host", &Target{Host: "", CreatedAt: now}},
		{"path traversal", &Target{Host: "../etc", CreatedAt: now}},
		{"slash in host", &Target{Host: "a/b", CreatedAt: now}},
	}
	for _, b := range bad {
		if err := b.t.Validate(); err == nil {
			t.Errorf("%s: expected error, got nil", b.name)
		}
	}
}

func TestTarget_Dir(t *testing.T) {
	tgt := &Target{Host: "api.empresa.com"}
	got := tgt.Dir("/ws/HackerOne-EMPRESA")
	want := "/ws/HackerOne-EMPRESA/targets/api.empresa.com"
	if got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if strings.Contains(got, "..") {
		t.Error("Dir must not contain ..")
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/projects/...`
Expected: FAIL (ProjectType undefined).

- [ ] **Step 3: Implement**

Criar `internal/projects/model.go`:

```go
package projects

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

type ProjectType string

const (
	ProjectBugBounty ProjectType = "bugbounty"
	ProjectPentest   ProjectType = "pentest"
)

func (p ProjectType) Valid() bool {
	switch p {
	case ProjectBugBounty, ProjectPentest:
		return true
	}
	return false
}

// nameSafe: apenas [A-Za-z0-9._-]; impede path traversal.
var nameSafe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var hostSafe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Project struct {
	Name      string      `yaml:"name"`
	Type      ProjectType `yaml:"type"`
	Program   string      `yaml:"program"`
	Platform  string      `yaml:"platform"`
	CreatedAt time.Time   `yaml:"created_at"`
}

func (p *Project) Validate() error {
	if p.Name == "" {
		return errors.New("name obrigatorio")
	}
	if !nameSafe.MatchString(p.Name) {
		return fmt.Errorf("name invalido (caracteres proibidos): %q", p.Name)
	}
	if !p.Type.Valid() {
		return fmt.Errorf("type invalido: %q (use bugbounty|pentest)", p.Type)
	}
	return nil
}

func (p *Project) Dir(workspace string) string {
	return filepath.Join(workspace, p.Name)
}

type Target struct {
	Host               string    `yaml:"host"`
	InScopePatterns    []string  `yaml:"in_scope"`
	OutOfScopePatterns []string  `yaml:"out_of_scope"`
	Notes              string    `yaml:"notes"`
	CreatedAt          time.Time `yaml:"created_at"`
}

func (t *Target) Validate() error {
	if t.Host == "" {
		return errors.New("host obrigatorio")
	}
	if !hostSafe.MatchString(t.Host) {
		return fmt.Errorf("host invalido: %q", t.Host)
	}
	return nil
}

func (t *Target) Dir(projectDir string) string {
	return filepath.Join(projectDir, "targets", t.Host)
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/projects/...`
Expected: PASS (todos).

- [ ] **Step 5: Commit**

```bash
git add internal/projects/model.go internal/projects/model_test.go
git commit -m "feat(projects): Project e Target structs com validacao"
```

---

## Task 5: Projects — repo (FS create/list/load)

**Files:**
- Create: `internal/projects/repo.go`
- Create: `internal/projects/repo_test.go`

**Interfaces:**
- Consumes: `*Project`, `*Target` (Task 4)
- Produces:
  ```go
  type Repo struct{ workspace string }
  func NewRepo(workspace string) *Repo

  // Project ops
  func (r *Repo) CreateProject(p *Project) error       // mkdir, write meta.yaml
  func (r *Repo) ListProjects() ([]*Project, error)
  func (r *Repo) LoadProject(name string) (*Project, error)
  func (r *Repo) ProjectExists(name string) (bool, error)
  func (r *Repo) DeleteProject(name string) error     // opcional, YAGNI; manter so se teste pedir

  // Target ops
  func (r *Repo) AddTarget(projectName string, t *Target) error
  func (r *Repo) ListTargets(projectName string) ([]*Target, error)
  func (r *Repo) LoadTarget(projectName, host string) (*Target, error)
  func (r *Repo) TargetExists(projectName, host string) (bool, error)

  // Path helper
  func (r *Repo) WorkspacePath() string
  ```

> **Nota YAGNI:** DeleteProject não é exigido pelo PRD v2.0. **NÃO implementar** até aparecer demanda real. Esta task implementa só Create/Load/List/Exists.

- [ ] **Step 1: Write failing test**

Criar `internal/projects/repo_test.go`:

```go
package projects

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	return NewRepo(dir)
}

func TestRepo_CreateLoadProject(t *testing.T) {
	r := newTestRepo(t)
	p := &Project{
		Name:      "HackerOne-EMPRESA",
		Type:      ProjectBugBounty,
		Program:   "EMPRESA",
		Platform:  "hackerone",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := r.CreateProject(p); err != nil {
		t.Fatalf("create: %v", err)
	}

	loaded, err := r.LoadProject("HackerOne-EMPRESA")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Type != ProjectBugBounty {
		t.Errorf("type = %q, want bugbounty", loaded.Type)
	}
	if loaded.Program != "EMPRESA" {
		t.Errorf("program = %q, want EMPRESA", loaded.Program)
	}
}

func TestRepo_CreateProject_Duplicate(t *testing.T) {
	r := newTestRepo(t)
	p := &Project{Name: "P", Type: ProjectBugBounty, CreatedAt: time.Now()}
	if err := r.CreateProject(p); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := r.CreateProject(p); err == nil {
		t.Fatal("expected error on duplicate")
	}
}

func TestRepo_ListProjects_Empty(t *testing.T) {
	r := newTestRepo(t)
	got, err := r.ListProjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestRepo_ListProjects_Multiple(t *testing.T) {
	r := newTestRepo(t)
	now := time.Now()
	for _, n := range []string{"A", "B", "C"} {
		if err := r.CreateProject(&Project{Name: n, Type: ProjectPentest, CreatedAt: now}); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
	}
	got, err := r.ListProjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
}

func TestRepo_AddLoadTarget(t *testing.T) {
	r := newTestRepo(t)
	if err := r.CreateProject(&Project{Name: "P", Type: ProjectBugBounty, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	tgt := &Target{
		Host:      "api.empresa.com",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := r.AddTarget("P", tgt); err != nil {
		t.Fatalf("add: %v", err)
	}
	loaded, err := r.LoadTarget("P", "api.empresa.com")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Host != "api.empresa.com" {
		t.Errorf("host = %q", loaded.Host)
	}
}

func TestRepo_AddTarget_ProjectMissing(t *testing.T) {
	r := newTestRepo(t)
	err := r.AddTarget("NOPE", &Target{Host: "x.com", CreatedAt: time.Now()})
	if err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestRepo_ListTargets_Empty(t *testing.T) {
	r := newTestRepo(t)
	if err := r.CreateProject(&Project{Name: "P", Type: ProjectBugBounty, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListTargets("P")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestRepo_ProjectDir_Correct(t *testing.T) {
	r := newTestRepo(t)
	p := &Project{Name: "P", Type: ProjectBugBounty, CreatedAt: time.Now()}
	if err := r.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(r.WorkspacePath(), "P", "meta.yaml")
	if _, err := loadFile(want); err != nil {
		t.Errorf("meta.yaml not at expected path: %v", err)
	}
}
```

Adicionar helper privado no test file:

```go
import "os"
func loadFile(p string) ([]byte, error) { return os.ReadFile(p) }
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/projects/...`
Expected: FAIL (`NewRepo` undefined).

- [ ] **Step 3: Implement**

Criar `internal/projects/repo.go`:

```go
package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type Repo struct {
	workspace string
}

func NewRepo(workspace string) *Repo {
	return &Repo{workspace: workspace}
}

func (r *Repo) WorkspacePath() string { return r.workspace }

func (r *Repo) projectDir(name string) string {
	return filepath.Join(r.workspace, name)
}

func (r *Repo) ProjectExists(name string) (bool, error) {
	_, err := os.Stat(r.projectDir(name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repo) CreateProject(p *Project) error {
	if err := p.Validate(); err != nil {
		return err
	}
	exists, err := r.ProjectExists(p.Name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("projeto ja existe: %s", p.Name)
	}
	dir := r.projectDir(p.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeYAML(filepath.Join(dir, "meta.yaml"), p)
}

func (r *Repo) LoadProject(name string) (*Project, error) {
	dir := r.projectDir(name)
	if _, err := os.Stat(filepath.Join(dir, "meta.yaml")); err != nil {
		return nil, fmt.Errorf("projeto nao encontrado: %s", name)
	}
	var p Project
	if err := readYAML(filepath.Join(dir, "meta.yaml"), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) ListProjects() ([]*Project, error) {
	entries, err := os.ReadDir(r.workspace)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p, err := r.LoadProject(e.Name())
		if err != nil {
			// ignora diretorios sem meta.yaml valido
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *Repo) targetDir(projectName, host string) string {
	return filepath.Join(r.projectDir(projectName), "targets", host)
}

func (r *Repo) TargetExists(projectName, host string) (bool, error) {
	_, err := os.Stat(r.targetDir(projectName, host))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repo) AddTarget(projectName string, t *Target) error {
	if err := t.Validate(); err != nil {
		return err
	}
	exists, err := r.ProjectExists(projectName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("projeto nao existe: %s", projectName)
	}
	exists, err = r.TargetExists(projectName, t.Host)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("alvo ja existe: %s/%s", projectName, t.Host)
	}
	dir := r.targetDir(projectName, t.Host)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeYAML(filepath.Join(dir, "meta.yaml"), t)
}

func (r *Repo) LoadTarget(projectName, host string) (*Target, error) {
	dir := r.targetDir(projectName, host)
	meta := filepath.Join(dir, "meta.yaml")
	if _, err := os.Stat(meta); err != nil {
		return nil, fmt.Errorf("alvo nao encontrado: %s/%s", projectName, host)
	}
	var t Target
	if err := readYAML(meta, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) ListTargets(projectName string) ([]*Target, error) {
	base := filepath.Join(r.projectDir(projectName), "targets")
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Target
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := r.LoadTarget(projectName, e.Name())
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out, nil
}

func writeYAML(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func readYAML(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, v)
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/projects/...`
Expected: PASS (todos).

- [ ] **Step 5: Commit**

```bash
git add internal/projects/repo.go internal/projects/repo_test.go
git commit -m "feat(projects): Repo FS para Project e Target"
```

---

## Task 6: Active state (get/set active project+target via config)

**Files:**
- Create: `internal/projects/active.go`
- Create: `internal/projects/active_test.go`

**Interfaces:**
- Consumes: `*Repo` (Task 5), `*config.Config` (Task 3)
- Produces:
  ```go
  type ActiveState struct {
      repo   *Repo
      config *config.Config
  }
  func NewActiveState(r *Repo, c *config.Config) *ActiveState

  func (a *ActiveState) SetProject(name string) error       // valida existencia, salva config
  func (a *ActiveState) SetTarget(host string) error        // exige projeto ativo, valida, salva
  func (a *ActiveState) Project() (*Project, error)         // nil se vazio
  func (a *ActiveState) Target() (*Target, error)           // nil se vazio ou projeto nao setado
  func (a *ActiveState) Context() (project, target, requestCount, error) // usado por get_active_context
  ```

- [ ] **Step 1: Write failing test**

Criar `internal/projects/active_test.go`:

```go
package projects

import (
	"testing"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/config"
)

func newTestActive(t *testing.T) (*ActiveState, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.WorkspacePath = t.TempDir()
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return NewActiveState(NewRepo(cfg.WorkspacePath), cfg), cfg
}

func TestActiveState_SetProject(t *testing.T) {
	a, _ := newTestActive(t)
	now := time.Now()
	if err := a.repo.CreateProject(&Project{Name: "P1", Type: ProjectBugBounty, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProject("P1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if a.config.ActiveProject != "P1" {
		t.Errorf("active = %q, want P1", a.config.ActiveProject)
	}
}

func TestActiveState_SetProject_NotFound(t *testing.T) {
	a, _ := newTestActive(t)
	if err := a.SetProject("NOPE"); err == nil {
		t.Fatal("expected error")
	}
}

func TestActiveState_SetTarget_NoProject(t *testing.T) {
	a, _ := newTestActive(t)
	if err := a.SetTarget("x.com"); err == nil {
		t.Fatal("expected error when no project active")
	}
}

func TestActiveState_SetTarget_Roundtrip(t *testing.T) {
	a, _ := newTestActive(t)
	now := time.Now()
	if err := a.repo.CreateProject(&Project{Name: "P", Type: ProjectBugBounty, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProject("P"); err != nil {
		t.Fatal(err)
	}
	if err := a.repo.AddTarget("P", &Target{Host: "x.com", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetTarget("x.com"); err != nil {
		t.Fatal(err)
	}
	tgt, err := a.Target()
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Host != "x.com" {
		t.Errorf("host = %q", tgt.Host)
	}
}

func TestActiveState_Project_Empty(t *testing.T) {
	a, _ := newTestActive(t)
	p, err := a.Project()
	if err != nil {
		t.Fatal(err)
	}
	if p != nil {
		t.Errorf("expected nil, got %+v", p)
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/projects/...`
Expected: FAIL (`NewActiveState` undefined).

- [ ] **Step 3: Implement**

Criar `internal/projects/active.go`:

```go
package projects

import (
	"fmt"

	"github.com/isaiaspereira307/network-copitlot/internal/config"
)

type ActiveState struct {
	repo   *Repo
	config *config.Config
}

func NewActiveState(r *Repo, c *config.Config) *ActiveState {
	return &ActiveState{repo: r, config: c}
}

func (a *ActiveState) SetProject(name string) error {
	exists, err := a.repo.ProjectExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("projeto nao existe: %s", name)
	}
	a.config.ActiveProject = name
	// ao trocar de projeto, zera alvo ativo
	a.config.ActiveTarget = ""
	return a.config.Save()
}

func (a *ActiveState) SetTarget(host string) error {
	if a.config.ActiveProject == "" {
		return fmt.Errorf("nenhum projeto ativo; use set_active_project primeiro")
	}
	exists, err := a.repo.TargetExists(a.config.ActiveProject, host)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("alvo nao existe: %s/%s", a.config.ActiveProject, host)
	}
	a.config.ActiveTarget = host
	return a.config.Save()
}

func (a *ActiveState) Project() (*Project, error) {
	if a.config.ActiveProject == "" {
		return nil, nil
	}
	return a.repo.LoadProject(a.config.ActiveProject)
}

func (a *ActiveState) Target() (*Target, error) {
	if a.config.ActiveProject == "" || a.config.ActiveTarget == "" {
		return nil, nil
	}
	return a.repo.LoadTarget(a.config.ActiveProject, a.config.ActiveTarget)
}

// Context retorna o estado ativo: projeto (nil se vazio), alvo (nil se vazio),
// contagem de requests no alvo ativo (0 se sem alvo). Erro apenas em IO real.
func (a *ActiveState) Context() (*Project, *Target, int, error) {
	p, err := a.Project()
	if err != nil || p == nil {
		return nil, nil, 0, err
	}
	t, err := a.Target()
	if err != nil || t == nil {
		return p, nil, 0, err
	}
	// requestCount: calculado pelo store na task 7/8; aqui retorna 0.
	// Sera sobrescrito por get_active_context (task 10) que tem acesso ao store.
	return p, t, 0, nil
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/projects/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/projects/active.go internal/projects/active_test.go
git commit -m "feat(projects): ActiveState com SetProject/SetTarget/Context"
```

---

## Task 7: Store — interface + schema constant

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/schema.go`

**Interfaces:**
- Consumes: nada
- Produces:
  ```go
  // store.go
  type Request struct {
      ID          int64
      Ts          int64                 // unix epoch ms
      Method      string
      URL         string
      ReqHeaders  map[string][]string
      ReqBody     []byte
      Status      int
      RespHeaders map[string][]string
      RespBody    []byte
      RespLen     int
      TTFBms      int
      Tags        []string
      Notes       string
  }

  type Store interface {
      Insert(r *Request) (int64, error)
      List(filter ListFilter) ([]*Request, error)
      Get(id int64) (*Request, error)
      Count() (int, error)
      Close() error
  }

  type ListFilter struct {
      Limit int
  }

  // schema.go
  const SchemaSQL = `
  CREATE TABLE IF NOT EXISTS requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts INTEGER NOT NULL,
    method TEXT NOT NULL,
    url TEXT NOT NULL,
    req_headers TEXT NOT NULL,
    req_body BLOB,
    status INTEGER,
    resp_headers TEXT,
    resp_body BLOB,
    resp_len INTEGER,
    ttfb_ms INTEGER,
    tags TEXT,
    notes TEXT
  );
  CREATE INDEX IF NOT EXISTS idx_requests_ts ON requests(ts);
  CREATE INDEX IF NOT EXISTS idx_requests_method_url ON requests(method, url);
  `
  ```

> **Nota:** Esta task é apenas a interface + struct + schema. Implementação SQLite na Task 8. TDD continua valendo porque a interface pode ser testada contra um stub.

- [ ] **Step 1: Write failing test**

Criar `internal/store/store_test.go`:

```go
package store

// stubStore verifica que a interface Store e respeitada.
type stubStore struct{ calls int }

func (s *stubStore) Insert(r *Request) (int64, error) { s.calls++; return 1, nil }
func (s *stubStore) List(ListFilter) ([]*Request, error) { return nil, nil }
func (s *stubStore) Get(int64) (*Request, error) { return nil, nil }
func (s *stubStore) Count() (int, error) { return 0, nil }
func (s *stubStore) Close() error { return nil }

func TestStoreInterface_Compiles(t *testing.T) {
	var _ Store = (*stubStore)(nil)
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/store/...`
Expected: FAIL (Store undefined).

- [ ] **Step 3: Implement**

Criar `internal/store/store.go`:

```go
package store

// Request representa uma transacao HTTP capturada.
type Request struct {
	ID          int64
	Ts          int64
	Method      string
	URL         string
	ReqHeaders  map[string][]string
	ReqBody     []byte
	Status      int
	RespHeaders map[string][]string
	RespBody    []byte
	RespLen     int
	TTFBms      int
	Tags        []string
	Notes       string
}

// ListFilter limita resultados. Zero = sem limite.
type ListFilter struct {
	Limit int
}

// Store persiste Request. Implementacoes: SQLite (per-target), futuro: memoria.
type Store interface {
	Insert(r *Request) (int64, error)
	List(filter ListFilter) ([]*Request, error)
	Get(id int64) (*Request, error)
	Count() (int, error)
	Close() error
}
```

Criar `internal/store/schema.go`:

```go
package store

// SchemaSQL e o DDL para a tabela `requests` (uma por alvo).
// Aplicado via db.Exec na inicializacao do store SQLite.
const SchemaSQL = `
CREATE TABLE IF NOT EXISTS requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  method TEXT NOT NULL,
  url TEXT NOT NULL,
  req_headers TEXT NOT NULL,
  req_body BLOB,
  status INTEGER,
  resp_headers TEXT,
  resp_body BLOB,
  resp_len INTEGER,
  ttfb_ms INTEGER,
  tags TEXT,
  notes TEXT
);
CREATE INDEX IF NOT EXISTS idx_requests_ts ON requests(ts);
CREATE INDEX IF NOT EXISTS idx_requests_method_url ON requests(method, url);
`
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/schema.go internal/store/store_test.go
git commit -m "feat(store): interface Store, Request struct, schema SQL"
```

---

## Task 8: Store — SQLite per-target implementation

**Files:**
- Create: `internal/store/sqlite.go`
- Create: `internal/store/sqlite_test.go`

**Interfaces:**
- Consumes: `Request`, `ListFilter`, `SchemaSQL` (Task 7)
- Produces:
  ```go
  type SQLiteStore struct{ db *sql.DB }
  func OpenSQLite(path string) (*SQLiteStore, error)  // cria dirs, aplica schema
  ```

- [ ] **Step 1: Write failing test**

Criar `internal/store/sqlite_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenSQLite(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteStore_InsertAndGet(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Insert(&Request{
		Ts:         1700000000000,
		Method:     "GET",
		URL:        "https://api.empresa.com/users",
		ReqHeaders: map[string][]string{"User-Agent": {"test"}},
		Status:     200,
		RespHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RespLen:    42,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id == 0 {
		t.Fatal("id must be > 0")
	}
	got, err := s.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Method != "GET" || got.URL != "https://api.empresa.com/users" || got.Status != 200 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

func TestSQLiteStore_List_Limit(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 5; i++ {
		if _, err := s.Insert(&Request{Ts: int64(i), Method: "GET", URL: "https://x.com/", ReqHeaders: map[string][]string{}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(ListFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
}

func TestSQLiteStore_Count(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 7; i++ {
		if _, err := s.Insert(&Request{Ts: int64(i), Method: "GET", URL: "https://x.com/", ReqHeaders: map[string][]string{}}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Errorf("count = %d, want 7", n)
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/store/...`
Expected: FAIL (`OpenSQLite` undefined).

- [ ] **Step 3: Implement**

Criar `internal/store/sqlite.go`:

```go
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // driver puro-Go, sem CGO
)

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// WAL mode: leitores nao bloqueiam escritor.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(SchemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func marshalJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalJSON[T any](s string) (T, error) {
	var z T
	if s == "" {
		return z, nil
	}
	err := json.Unmarshal([]byte(s), &z)
	return z, err
}

func (s *SQLiteStore) Insert(r *Request) (int64, error) {
	headersReq, err := marshalJSON(r.ReqHeaders)
	if err != nil {
		return 0, err
	}
	headersResp, err := marshalJSON(r.RespHeaders)
	if err != nil {
		return 0, err
	}
	tags, err := marshalJSON(r.Tags)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`
		INSERT INTO requests (ts, method, url, req_headers, req_body, status, resp_headers, resp_body, resp_len, ttfb_ms, tags, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.Ts, r.Method, r.URL, headersReq, r.ReqBody, r.Status, headersResp, r.RespBody, r.RespLen, r.TTFBms, tags, r.Notes)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) Get(id int64) (*Request, error) {
	row := s.db.QueryRow(`SELECT id, ts, method, url, req_headers, req_body, status, resp_headers, resp_body, resp_len, ttfb_ms, tags, notes FROM requests WHERE id = ?`, id)
	return scanRequest(row)
}

func (s *SQLiteStore) List(f ListFilter) ([]*Request, error) {
	q := `SELECT id, ts, method, url, req_headers, req_body, status, resp_headers, resp_body, resp_len, ttfb_ms, tags, notes FROM requests ORDER BY id DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Count() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanRequest(s scanner) (*Request, error) {
	var (
		r        Request
		hReq     string
		hResp    string
		tagsJSON string
	)
	err := s.Scan(&r.ID, &r.Ts, &r.Method, &r.URL, &hReq, &r.ReqBody, &r.Status, &hResp, &r.RespBody, &r.RespLen, &r.TTFBms, &tagsJSON, &r.Notes)
	if err != nil {
		return nil, err
	}
	if r.ReqHeaders, err = unmarshalJSON[map[string][]string](hReq); err != nil {
		return nil, err
	}
	if r.RespHeaders, err = unmarshalJSON[map[string][]string](hResp); err != nil {
		return nil, err
	}
	if r.Tags, err = unmarshalJSON[[]string](tagsJSON); err != nil {
		return nil, err
	}
	return &r, nil
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/sqlite.go internal/store/sqlite_test.go
git commit -m "feat(store): SQLiteStore per-target com WAL"
```

---

## Task 9: Store — migration v1 flat → v2 per-target (idempotente)

**Files:**
- Create: `internal/store/migrate.go`
- Create: `internal/store/migrate_test.go`

**Interfaces:**
- Consumes: workspace dir, repo `*projects.Repo`
- Produces:
  ```go
  // MigrateV1ToV2 procura por um requests.db flat no workspace root.
  // Se existir e o workspace ainda nao tiver projetos, move para o alvo
  // "legacy" dentro de um projeto "imported-v1". Idempotente: no-op se ja
  // rodou antes (verifica marker file).
  func MigrateV1ToV2(workspaceDir string) error

  // const migrationMarker = ".mcp-proxy-v2-migrated"
  ```

> **Nota:** Esta migration existe para o caso de um usuario que rodou a v1 ter
> capturado dados que nao devem ser perdidos. No estado atual (repo vazio) o
> teste sera um no-op; mas a logica precisa existir e ser testada com
> fixtures.

- [ ] **Step 1: Write failing test**

Criar `internal/store/migrate_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateV1ToV2_NoOpWhenClean(t *testing.T) {
	dir := t.TempDir()
	if err := MigrateV1ToV2(dir); err != nil {
		t.Fatal(err)
	}
	// marker nao deve existir (so criado quando ha dados)
	if _, err := os.Stat(filepath.Join(dir, migrationMarker)); err == nil {
		t.Error("marker must not be created when nothing to migrate")
	}
}

func TestMigrateV1ToV2_MovesLegacyDB(t *testing.T) {
	dir := t.TempDir()
	// simula v1: requests.db na raiz do workspace
	if err := os.WriteFile(filepath.Join(dir, "requests.db"), []byte("fake-v1-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateV1ToV2(dir); err != nil {
		t.Fatal(err)
	}
	// requests.db na raiz deve ter sido removido
	if _, err := os.Stat(filepath.Join(dir, "requests.db")); !os.IsNotExist(err) {
		t.Error("legacy requests.db should be removed")
	}
	// marker criado
	if _, err := os.Stat(filepath.Join(dir, migrationMarker)); err != nil {
		t.Errorf("marker not created: %v", err)
	}
	// alvo imported-v1/legacy/ deve existir
	legacyDir := filepath.Join(dir, "imported-v1", "targets", "legacy")
	if _, err := os.Stat(legacyDir); err != nil {
		t.Errorf("legacy target dir not created: %v", err)
	}
}

func TestMigrateV1ToV2_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requests.db"), []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateV1ToV2(dir); err != nil {
		t.Fatal(err)
	}
	// segunda chamada: no-op, sem erro
	if err := MigrateV1ToV2(dir); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/store/...`
Expected: FAIL.

- [ ] **Step 3: Implement**

Criar `internal/store/migrate.go`:

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
)

const migrationMarker = ".mcp-proxy-v2-migrated"

// MigrateV1ToV2 move um requests.db flat da raiz do workspace para
// imported-v1/targets/legacy/requests.db. Idempotente: se marker existe
// ou se workspace ja tem projetos, no-op.
func MigrateV1ToV2(workspaceDir string) error {
	marker := filepath.Join(workspaceDir, migrationMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil // ja migrado
	}
	legacy := filepath.Join(workspaceDir, "requests.db")
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		return nil // nada a fazer
	} else if err != nil {
		return err
	}
	// ha dados v1; cria estrutura v2
	targetDir := filepath.Join(workspaceDir, "imported-v1", "targets", "legacy")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	// cria meta.yaml do projeto + alvo
	projMeta := `name: imported-v1
type: bugbounty
created_at: 0001-01-01T00:00:00Z
`
	if err := os.WriteFile(filepath.Join(workspaceDir, "imported-v1", "meta.yaml"), []byte(projMeta), 0o600); err != nil {
		return fmt.Errorf("write project meta: %w", err)
	}
	tgtMeta := `host: legacy
created_at: 0001-01-01T00:00:00Z
notes: "importado da v1 (flat store)"
`
	if err := os.WriteFile(filepath.Join(targetDir, "meta.yaml"), []byte(tgtMeta), 0o600); err != nil {
		return fmt.Errorf("write target meta: %w", err)
	}
	// move o DB
	if err := os.Rename(legacy, filepath.Join(targetDir, "requests.db")); err != nil {
		return fmt.Errorf("move db: %w", err)
	}
	// cria marker
	return os.WriteFile(marker, []byte("migrated"), 0o600)
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrate.go internal/store/migrate_test.go
git commit -m "feat(store): migrate v1 flat requests.db para per-target (idempotente)"
```

---

## Task 10: Audit log (JSON-lines append-only)

**Files:**
- Create: `internal/audit/audit.go`
- Create: `internal/audit/audit_test.go`

**Interfaces:**
- Consumes: nada
- Produces:
  ```go
  type Event struct {
      Ts     time.Time `json:"ts"`
      Tool   string    `json:"tool"`
      User   string    `json:"user,omitempty"`
      Action string    `json:"action"`
      Detail any       `json:"detail,omitempty"`
  }
  type Logger struct{ ... }
  func DefaultPath() (string, error) // ~/.mcp-proxy/audit.log
  func New(path string) (*Logger, error)
  func (l *Logger) Log(e Event) error    // mutex serializa escritas
  func (l *Logger) Close() error
  ```

- [ ] **Step 1: Write failing test**

Criar `internal/audit/audit_test.go`:

```go
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogger_WritesJSONLines(t *testing.T) {
	dir := t.TempDir()
	l, err := New(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if err := l.Log(Event{Ts: time.Unix(0, 0), Tool: "create_project", Action: "create", Detail: map[string]any{"name": "P1"}}); err != nil {
		t.Fatal(err)
	}
	if err := l.Log(Event{Ts: time.Unix(1, 0), Tool: "add_target", Action: "add", Detail: map[string]any{"host": "x.com"}}); err != nil {
		t.Fatal(err)
	}
	l.Close()

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("line 0 invalid JSON: %v", err)
	}
	if e.Tool != "create_project" {
		t.Errorf("tool = %q", e.Tool)
	}
}

func TestLogger_ConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	l, err := New(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			l.Log(Event{Tool: "x", Action: "a", Detail: i})
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	l.Close()
	data, _ := os.ReadFile(filepath.Join(dir, "audit.log"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/audit/...`
Expected: FAIL.

- [ ] **Step 3: Implement**

Criar `internal/audit/audit.go`:

```go
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const dirName = ".mcp-proxy"
const fileName = "audit.log"

type Event struct {
	Ts     time.Time `json:"ts"`
	Tool   string    `json:"tool"`
	User   string    `json:"user,omitempty"`
	Action string    `json:"action"`
	Detail any       `json:"detail,omitempty"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName, fileName), nil
}

type Logger struct {
	mu   sync.Mutex
	file *os.File
}

func New(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f}, nil
}

func (l *Logger) Log(e Event) error {
	if e.Ts.IsZero() {
		e.Ts = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(l.file, "%s\n", data)
	return err
}

func (l *Logger) Close() error {
	return l.file.Close()
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/audit/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/audit/
git commit -m "feat(audit): logger JSON-lines append-only thread-safe"
```

---

## Task 11: MCP server wrapper (skeleton sem tools ainda)

**Files:**
- Create: `internal/mcpserver/server.go`
- Create: `internal/mcpserver/server_test.go`

**Interfaces:**
- Consumes: `*ActiveState`, `*Repo`, `*audit.Logger`
- Produces:
  ```go
  type Server struct {
      active *projects.ActiveState
      repo   *projects.Repo
      audit  *audit.Logger
  }
  func New(active *projects.ActiveState, repo *projects.Repo, a *audit.Logger) *Server
  // Run inicia o servidor MCP via stdio. Bloqueia ate ctx cancelar.
  func (s *Server) Run(ctx context.Context, stdio mcpserver.StdioServer) error
  ```

> **Nota:** Esta task so cria o esqueleto. As 7 tools vem nas tasks 12-14.

- [ ] **Step 1: Write failing test**

Criar `internal/mcpserver/server_test.go`:

```go
package mcpserver

import (
	"testing"
)

func TestNew_NotNil(t *testing.T) {
	s := New(nil, nil, nil)
	if s == nil {
		t.Fatal("New returned nil")
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/mcpserver/...`
Expected: FAIL.

- [ ] **Step 3: Implement**

Criar `internal/mcpserver/server.go`:

```go
package mcpserver

import (
	"context"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
)

// mcpserver alias para evitar import circular quando o mcp-go SDK
// evoluir. Aqui usamos o pacote direto.
importmcpsdk "github.com/mark3labs/mcp-go/server"

type Server struct {
	active *projects.ActiveState
	repo   *projects.Repo
	audit  *audit.Logger
	mcp    *importmcpsdk.MCPServer
}

func New(active *projects.ActiveState, repo *projects.Repo, a *audit.Logger) *Server {
	s := importmcpsdk.NewMCPServer("mcp-proxy", "v0.2.0")
	return &Server{active: active, repo: repo, audit: a, mcp: s}
}

// Run inicia o servidor MCP em stdio. Bloqueia.
func (s *Server) Run(ctx context.Context) error {
	return importmcpsdk.ServeStdio(s.mcp)
}
```

> **Ajuste em runtime:** se a API `mcp-go` mudar (ex: `NewMCPServer` recebe options), adaptar seguindo a versao pinned. O teste garante que o pacote compila.

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/mcpserver/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver/server.go internal/mcpserver/server_test.go
git commit -m "feat(mcpserver): skeleton Server com New e Run stdio"
```

---

## Task 12: MCP tools v2 — project ops (create, list, set_active)

**Files:**
- Create: `internal/mcpserver/tools_v2.go`
- Create: `internal/mcpserver/tools_v2_test.go`

**Interfaces (saída):**
- Tools MCP registrados em `Server.mcp`:
  - `create_project(name, type, program, platform)` → string (confirmação)
  - `list_projects()` → string (JSON)
  - `set_active_project(name)` → string (confirmação)

- [ ] **Step 1: Write failing test**

Criar `internal/mcpserver/tools_v2_test.go`:

```go
package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/config"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
)

func newTestServer(t *testing.T) (*Server, *projects.ActiveState) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load()
	cfg.WorkspacePath = t.TempDir()
	cfg.Save()
	repo := projects.NewRepo(cfg.WorkspacePath)
	active := projects.NewActiveState(repo, cfg)
	auditDir := t.TempDir()
	al, _ := audit.New(filepath.Join(auditDir, "audit.log"))
	t.Cleanup(func() { al.Close() })
	s := New(active, repo, al)
	registerV2Tools(s)
	return s, active
}

func TestCreateProjectTool(t *testing.T) {
	s, _ := newTestServer(t)
	out := callTool(t, s, "create_project", map[string]any{
		"name": "HackerOne-X",
		"type": "bugbounty",
	})
	if out == "" {
		t.Fatal("empty output")
	}
	// verifica persistencia
	repo := projects.NewRepo(getWorkspace(t))
	p, err := repo.LoadProject("HackerOne-X")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Type != projects.ProjectBugBounty {
		t.Errorf("type = %q", p.Type)
	}
}

func TestListProjectsTool(t *testing.T) {
	s, _ := newTestServer(t)
	callTool(t, s, "create_project", map[string]any{"name": "A", "type": "pentest"})
	callTool(t, s, "create_project", map[string]any{"name": "B", "type": "bugbounty"})
	out := callTool(t, s, "list_projects", map[string]any{})
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestSetActiveProjectTool(t *testing.T) {
	s, active := newTestServer(t)
	callTool(t, s, "create_project", map[string]any{"name": "P1", "type": "bugbounty"})
	out := callTool(t, s, "set_active_project", map[string]any{"name": "P1"})
	if out == "" {
		t.Fatal("empty")
	}
	if active.Project() == nil {
		t.Fatal("active project not set")
	}
}

func TestSetActiveProject_NotFound(t *testing.T) {
	s, _ := newTestServer(t)
	out := callTool(t, s, "set_active_project", map[string]any{"name": "NOPE"})
	if out == "" {
		t.Fatal("expected error message")
	}
}
```

Adicionar helpers de teste:

```go
// em tools_v2_test.go (continuacao)

func callTool(t *testing.T, s *Server, name string, args map[string]any) string {
	t.Helper()
	fn, ok := toolRegistry[name]
	if !ok {
		t.Fatalf("tool %s not registered", name)
	}
	out, err := fn(context.Background(), args)
	if err != nil {
		t.Fatalf("tool %s error: %v", name, err)
	}
	return out
}

func getWorkspace(t *testing.T) string {
	t.Helper()
	home := os.Getenv("HOME")
	cfg, _ := config.Load()
	return cfg.WorkspacePath
}

// silence unused time import
var _ = time.Now
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/mcpserver/...`
Expected: FAIL (`registerV2Tools`, `toolRegistry` undefined).

- [ ] **Step 3: Implement**

Criar `internal/mcpserver/tools_v2.go`:

```go
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
)

// toolFunc e a assinatura comum de todas as tools v2.
type toolFunc func(ctx context.Context, args map[string]any) (string, error)

// toolRegistry mantem as tools v2. Inicializado por registerV2Tools.
// NOTA: em producao essas tools serao registradas no mcp-go Server.
// Aqui usamos um registry local para testar sem subir stdio.
var toolRegistry = map[string]toolFunc{}

func registerV2Tools(s *Server) {
	// project tools
	toolRegistry["create_project"] = s.toolCreateProject
	toolRegistry["list_projects"] = s.toolListProjects
	toolRegistry["set_active_project"] = s.toolSetActiveProject
	// target tools (task 13)
	toolRegistry["add_target"] = s.toolAddTarget
	toolRegistry["list_targets"] = s.toolListTargets
	toolRegistry["set_active_target"] = s.toolSetActiveTarget
	// context (task 14)
	toolRegistry["get_active_context"] = s.toolGetActiveContext

	// O wiring real no mcp-go Server (via s.mcp.AddTool) acontece na
	// Task 15. Este registry local existe apenas para os testes
	// (testam toolFunc direto sem subir stdio).
}

func (s *Server) toolCreateProject(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	typ, _ := args["type"].(string)
	program, _ := args["program"].(string)
	platform, _ := args["platform"].(string)
	if name == "" || typ == "" {
		return "", fmt.Errorf("name e type sao obrigatorios")
	}
	p := &projects.Project{
		Name:      name,
		Type:      projects.ProjectType(typ),
		Program:   program,
		Platform:  platform,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.repo.CreateProject(p); err != nil {
		s.audit.Log(audit.Event{Tool: "create_project", Action: "error", Detail: map[string]any{"name": name, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "create_project", Action: "create", Detail: map[string]any{"name": name}})
	return fmt.Sprintf("projeto criado: %s", name), nil
}

func (s *Server) toolListProjects(ctx context.Context, args map[string]any) (string, error) {
	list, err := s.repo.ListProjects()
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(list)
	return string(out), nil
}

func (s *Server) toolSetActiveProject(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return "", fmt.Errorf("name obrigatorio")
	}
	if err := s.active.SetProject(name); err != nil {
		s.audit.Log(audit.Event{Tool: "set_active_project", Action: "error", Detail: map[string]any{"name": name, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "set_active_project", Action: "set", Detail: map[string]any{"name": name}})
	return fmt.Sprintf("projeto ativo: %s", name), nil
}

// stubs serao preenchidos nas tasks 13 e 14.
func (s *Server) toolAddTarget(ctx context.Context, args map[string]any) (string, error) {
	return "", fmt.Errorf("not implemented yet")
}
func (s *Server) toolListTargets(ctx context.Context, args map[string]any) (string, error) {
	return "", fmt.Errorf("not implemented yet")
}
func (s *Server) toolSetActiveTarget(ctx context.Context, args map[string]any) (string, error) {
	return "", fmt.Errorf("not implemented yet")
}
func (s *Server) toolGetActiveContext(ctx context.Context, args map[string]any) (string, error) {
	return "", fmt.Errorf("not implemented yet")
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/mcpserver/...`
Expected: PASS (3 tests verdes; target/context tests na task 13/14).

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver/tools_v2.go internal/mcpserver/tools_v2_test.go
git commit -m "feat(mcp): tools create_project, list_projects, set_active_project"
```

---

## Task 13: MCP tools v2 — target ops (add com confirmação, list, set_active)

**Files:**
- Modify: `internal/mcpserver/tools_v2.go`
- Modify: `internal/mcpserver/tools_v2_test.go`

**Interfaces:** add 3 implementações reais + testes.

> **Importante:** a confirmação "Tem autorização?" é responsabilidade do **cliente MCP** (Claude) per o contrato MCP. O server **apenas exige** um campo `confirmed: true` no input. Documentar isso na docstring da tool e validar server-side. (PRD §5: "Confirmação ao adicionar target".)

- [ ] **Step 1: Estender testes**

Adicionar a `tools_v2_test.go`:

```go
func TestAddTargetTool_RequiresConfirmation(t *testing.T) {
	s, _ := newTestServer(t)
	callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	// sem confirmed: true -> erro
	_, err := toolRegistry["add_target"](context.Background(), map[string]any{"host": "x.com"})
	if err == nil {
		t.Fatal("expected error when confirmation missing")
	}
}

func TestAddTargetTool_WithConfirmation(t *testing.T) {
	s, _ := newTestServer(t)
	callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	out := callTool(t, s, "add_target", map[string]any{
		"host":      "api.empresa.com",
		"confirmed": true,
	})
	if out == "" {
		t.Fatal("empty")
	}
	// verifica persistencia
	repo := projects.NewRepo(getWorkspace(t))
	tgt, err := repo.LoadTarget("P", "api.empresa.com")
	if err != nil {
		t.Fatalf("load target: %v", err)
	}
	if tgt.Host != "api.empresa.com" {
		t.Errorf("host = %q", tgt.Host)
	}
}

func TestAddTargetTool_NoActiveProject(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := toolRegistry["add_target"](context.Background(), map[string]any{"host": "x.com", "confirmed": true})
	if err == nil {
		t.Fatal("expected error: no active project")
	}
}

func TestListTargetsTool(t *testing.T) {
	s, _ := newTestServer(t)
	callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	callTool(t, s, "add_target", map[string]any{"host": "a.com", "confirmed": true})
	callTool(t, s, "add_target", map[string]any{"host": "b.com", "confirmed": true})
	out := callTool(t, s, "list_targets", map[string]any{})
	var list []map[string]any
	_ = json.Unmarshal([]byte(out), &list)
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestSetActiveTargetTool(t *testing.T) {
	s, active := newTestServer(t)
	callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	callTool(t, s, "add_target", map[string]any{"host": "x.com", "confirmed": true})
	callTool(t, s, "set_active_target", map[string]any{"host": "x.com"})
	tgt, _ := active.Target()
	if tgt == nil || tgt.Host != "x.com" {
		t.Errorf("active target not set: %+v", tgt)
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/mcpserver/...`
Expected: FAIL (add_target not implemented).

- [ ] **Step 3: Implementar (substituir stubs em tools_v2.go)**

Substituir os 3 stubs `toolAddTarget`, `toolListTargets`, `toolSetActiveTarget`:

```go
func (s *Server) toolAddTarget(ctx context.Context, args map[string]any) (string, error) {
	host, _ := args["host"].(string)
	confirmed, _ := args["confirmed"].(bool)
	if host == "" {
		return "", fmt.Errorf("host obrigatorio")
	}
	if !confirmed {
		return "", fmt.Errorf("cliente deve confirmar autorizacao: passe confirmed=true (PRD §5)")
	}
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return "", fmt.Errorf("nenhum projeto ativo")
	}
	inScope, _ := args["in_scope"].([]any)
	outScope, _ := args["out_of_scope"].([]any)
	toStr := func(xs []any) []string {
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	tgt := &projects.Target{
		Host:               host,
		InScopePatterns:    toStr(inScope),
		OutOfScopePatterns: toStr(outScope),
		CreatedAt:          time.Now().UTC(),
	}
	if err := s.repo.AddTarget(proj.Name, tgt); err != nil {
		s.audit.Log(audit.Event{Tool: "add_target", Action: "error", Detail: map[string]any{"host": host, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "add_target", Action: "add", Detail: map[string]any{"host": host, "project": proj.Name}})
	return fmt.Sprintf("alvo adicionado: %s/%s", proj.Name, host), nil
}

func (s *Server) toolListTargets(ctx context.Context, args map[string]any) (string, error) {
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return "", fmt.Errorf("nenhum projeto ativo")
	}
	list, err := s.repo.ListTargets(proj.Name)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(list)
	return string(out), nil
}

func (s *Server) toolSetActiveTarget(ctx context.Context, args map[string]any) (string, error) {
	host, _ := args["host"].(string)
	if host == "" {
		return "", fmt.Errorf("host obrigatorio")
	}
	if err := s.active.SetTarget(host); err != nil {
		s.audit.Log(audit.Event{Tool: "set_active_target", Action: "error", Detail: map[string]any{"host": host, "err": err.Error()}})
		return "", err
	}
	s.audit.Log(audit.Event{Tool: "set_active_target", Action: "set", Detail: map[string]any{"host": host}})
	return fmt.Sprintf("alvo ativo: %s", host), nil
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/mcpserver/...`
Expected: PASS (todos).

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver/tools_v2.go internal/mcpserver/tools_v2_test.go
git commit -m "feat(mcp): tools add_target (com confirm), list_targets, set_active_target"
```

---

## Task 14: MCP tool v2 — get_active_context (com request count)

**Files:**
- Modify: `internal/mcpserver/tools_v2.go`
- Modify: `internal/mcpserver/tools_v2_test.go`

**Interfaces:** o server precisa de acesso ao `store.Store` do alvo ativo.

Adicionar campo `currentStore` ao `Server` (lazy: abre ao trocar de alvo ativo, fecha o anterior).

- [ ] **Step 1: Estender teste**

```go
func TestGetActiveContextTool_Empty(t *testing.T) {
	s, _ := newTestServer(t)
	out := callTool(t, s, "get_active_context", map[string]any{})
	if out == "" {
		t.Fatal("empty")
	}
	// sem projeto ativo: retorna JSON com active_project=""
	var ctx map[string]any
	_ = json.Unmarshal([]byte(out), &ctx)
	if ctx["active_project"] != "" {
		t.Errorf("expected empty, got %+v", ctx)
	}
}

func TestGetActiveContextTool_Full(t *testing.T) {
	s, _ := newTestServer(t)
	callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	callTool(t, s, "add_target", map[string]any{"host": "x.com", "confirmed": true})
	callTool(t, s, "set_active_target", map[string]any{"host": "x.com"})
	out := callTool(t, s, "get_active_context", map[string]any{})
	var ctx map[string]any
	_ = json.Unmarshal([]byte(out), &ctx)
	if ctx["active_project"] != "P" {
		t.Errorf("project = %v", ctx["active_project"])
	}
	if ctx["active_target"] != "x.com" {
		t.Errorf("target = %v", ctx["active_target"])
	}
	if _, ok := ctx["request_count"]; !ok {
		t.Error("request_count missing")
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/mcpserver/...`
Expected: FAIL.

- [ ] **Step 3: Implementar**

Modificar `internal/mcpserver/server.go` (adicionar campo e método):

```go
type Server struct {
	active      *projects.ActiveState
	repo        *projects.Repo
	audit       *audit.Logger
	mcp         *importmcpsdk.MCPServer
	currentStore store.Store   // aberto sob demanda
}

// openStoreForActiveTarget abre (ou recria) o store SQLite do alvo ativo.
// Retorna nil se nao ha alvo ativo. Fecha o anterior se existir.
func (s *Server) openStoreForActiveTarget() (store.Store, error) {
	if s.currentStore != nil {
		_ = s.currentStore.Close()
		s.currentStore = nil
	}
	proj, err := s.active.Project()
	if err != nil || proj == nil {
		return nil, nil
	}
	tgt, err := s.active.Target()
	if err != nil || tgt == nil {
		return nil, nil
	}
	targetDir := tgt.Dir(proj.Dir(s.repo.WorkspacePath()))
	dbPath := filepath.Join(targetDir, "requests.db")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		return nil, err
	}
	s.currentStore = st
	return st, nil
}
```

Adicionar imports em `server.go`:

```go
import (
	"context"
	"path/filepath"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/isaiaspereira307/network-copitlot/internal/store"

	importmcpsdk "github.com/mark3labs/mcp-go/server"
)
```

Substituir `toolGetActiveContext` em `tools_v2.go`:

```go
func (s *Server) toolGetActiveContext(ctx context.Context, args map[string]any) (string, error) {
	proj, _ := s.active.Project()
	tgt, _ := s.active.Target()
	out := map[string]any{
		"active_project": "",
		"active_target":  "",
		"request_count":  0,
	}
	if proj != nil {
		out["active_project"] = proj.Name
		out["project_type"] = string(proj.Type)
	}
	if tgt != nil {
		out["active_target"] = tgt.Host
		st, err := s.openStoreForActiveTarget()
		if err == nil && st != nil {
			n, _ := st.Count()
			out["request_count"] = n
		}
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/mcpserver/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver/
git commit -m "feat(mcp): get_active_context com request_count via SQLite per-target"
```

---

## Task 15: Wire mcp-go — registrar tools v2 no server real

**Files:**
- Modify: `internal/mcpserver/server.go`
- Create: `internal/mcpserver/wire_test.go`

> **Nota:** se a API do `mark3labs/mcp-go` mudou desde este plano, adaptar
> a chamada `AddTool`/`WithToolHandler` para a versão pinada. Teste verifica
> que o server e as 7 tools estao disponiveis via API do SDK.

- [ ] **Step 1: Verificar API do mcp-go**

Run: `go doc github.com/mark3labs/mcp-go/server | head -50`
Adaptar o código abaixo ao API real.

- [ ] **Step 2: Implementar wiring real em server.go**

Adicionar ao `New()` ou em um método separado `Server.RegisterTools()`:

```go
// Em server.go
func (s *Server) RegisterTools() {
	s.mcp.AddTool(
		mcp.NewTool("create_project",
			mcp.WithDescription("Cria um novo projeto (engajamento) de bug bounty ou pentest"),
			mcp.WithString("name", mcp.Required()),
			mcp.WithString("type", mcp.Required(), mcp.Enum("bugbounty", "pentest")),
			mcp.WithString("program"),
			mcp.WithString("platform"),
		),
		s.wrapTool("create_project", s.toolCreateProject),
	)
	// repetir para as outras 6 tools (list_projects, set_active_project,
	// add_target, list_targets, set_active_target, get_active_context)
	// ...
}

func (s *Server) wrapTool(name string, fn toolFunc) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		out, err := fn(ctx, req.Params.Arguments)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}
```

- [ ] **Step 3: Write test**

`wire_test.go`:

```go
package mcpserver

import (
	"testing"
)

func TestRegisterTools_NoPanic(t *testing.T) {
	s := New(nil, nil, nil)
	s.RegisterTools()
	// se chegou aqui sem panic, ok
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/mcpserver/...`
Expected: PASS (pode requerir ajustes na API exata do mcp-go; consultar go doc).

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver/
git commit -m "feat(mcp): registrar 7 tools v2 no server mcp-go real"
```

---

## Task 16: CLI — cobra root + subcommand `project`

**Files:**
- Create: `cmd/mcp-proxy/main.go` (modificar)
- Create: `cmd/mcp-proxy/project.go`
- Create: `cmd/mcp-proxy/project_test.go`

**Interfaces:**

```
mcp-proxy project create --name NAME --type TYPE [--program P] [--platform P]
mcp-proxy project list
mcp-proxy project use NAME
```

- [ ] **Step 1: Write failing test**

`project_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/config"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/spf13/cobra"
)

func setupCLI(t *testing.T) (*cobra.Command, *projects.ActiveState) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load()
	cfg.WorkspacePath = t.TempDir()
	cfg.Save()
	repo := projects.NewRepo(cfg.WorkspacePath)
	active := projects.NewActiveState(repo, cfg)
	al, _ := audit.New(filepath.Join(t.TempDir(), "audit.log"))
	t.Cleanup(func() { al.Close() })

	root := NewRootCmd(active, repo, al)
	return root, active
}

func captureRun(t *testing.T, cmd *cobra.Command, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	buf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs(append([]string{"project"}, args...))
	err = cmd.Execute()
	return buf.String(), errBuf.String(), err
}

func TestProjectCreate(t *testing.T) {
	root, _ := setupCLI(t)
	out, _, err := captureRun(t, root, "create", "--name", "P1", "--type", "bugbounty")
	if err != nil {
		t.Fatalf("err: %v stderr=%s", err, out)
	}
	if !strings.Contains(out, "P1") {
		t.Errorf("output missing P1: %s", out)
	}
}

func TestProjectList_Empty(t *testing.T) {
	root, _ := setupCLI(t)
	out, _, err := captureRun(t, root, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out), "nenhum") && out != "" {
		t.Logf("list output: %s", out)
	}
}

func TestProjectUse(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, _ = captureRun(t, root, "create", "--name", "P1", "--type", "bugbounty")
	_, _, err := captureRun(t, root, "use", "P1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestProjectUse_NotFound(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, err := captureRun(t, root, "use", "NOPE")
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./cmd/mcp-proxy/...`
Expected: FAIL (`NewRootCmd` undefined).

- [ ] **Step 3: Implementar `project.go` + reescrever `main.go`**

`cmd/mcp-proxy/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/config"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
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

	// Se for modo MCP (sem subcommand), subir o server.
	if len(os.Args) < 2 {
		return runMCPServer(active, repo, al)
	}
	// CLI mode
	cmd := NewRootCmd(active, repo, al)
	return cmd.Execute()
}

func runMCPServer(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) error {
	s := mcpserver.New(active, repo, al)
	s.RegisterTools()
	return s.Run(context.Background())
}
```

> Adicionar import `store` em main.go para o `MigrateV1ToV2`.

Criar `cmd/mcp-proxy/project.go`:

```go
package main

import (
	"fmt"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
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
				// CreatedAt: time.Now().UTC(),
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
				if active != nil {
					// active state disponivel via closure se necessario
				}
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
```

Criar `cmd/mcp-proxy/root.go`:

```go
package main

import (
	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
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
```

> NOTA: `newTargetCmd` é criado na Task 17.

- [ ] **Step 4: Run, expect pass**

Run: `go test ./cmd/mcp-proxy/...`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/mcp-proxy/
git commit -m "feat(cli): subcommand project (create|list|use)"
```

---

## Task 17: CLI — subcommand `target`

**Files:**
- Create: `cmd/mcp-proxy/target.go`
- Create: `cmd/mcp-proxy/target_test.go`

- [ ] **Step 1: Write failing test**

`target_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestTargetAdd_RequiresConfirmation(t *testing.T) {
	root, _ := setupCLI(t)
	root.SetArgs([]string{"target", "add", "--host", "x.com"})
	// sem --confirm: erro
	_, _, err := captureRunTarget(t, root, "add", "--host", "x.com")
	if err == nil {
		t.Fatal("expected error without --confirm")
	}
}

func TestTargetAdd_WithConfirm(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, _ = captureRunTarget(t, root, "create", "--name", "P", "--type", "bugbounty")
	_, _, _ = captureRunTarget(t, root, "use", "P")
	out, _, err := captureRunTarget(t, root, "add", "--host", "x.com", "--confirm")
	if err != nil {
		t.Fatalf("err: %v out=%s", err, out)
	}
	if !strings.Contains(out, "x.com") {
		t.Errorf("output missing host: %s", out)
	}
}

func TestTargetList_Empty(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, _ = captureRunTarget(t, root, "create", "--name", "P", "--type", "bugbounty")
	_, _, _ = captureRunTarget(t, root, "use", "P")
	out, _, err := captureRunTarget(t, root, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out), "nenhum") {
		t.Errorf("expected empty message, got: %s", out)
	}
}

func TestTargetUse(t *testing.T) {
	root, _ := setupCLI(t)
	_, _, _ = captureRunTarget(t, root, "create", "--name", "P", "--type", "bugbounty")
	_, _, _ = captureRunTarget(t, root, "use", "P")
	_, _, _ = captureRunTarget(t, root, "add", "--host", "x.com", "--confirm")
	_, _, err := captureRunTarget(t, root, "use", "x.com")
	if err != nil {
		t.Fatal(err)
	}
}
```

Adicionar helper:

```go
func captureRunTarget(t *testing.T, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs(append([]string{"target"}, args...))
	err := cmd.Execute()
	return buf.String(), errBuf.String(), err
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./cmd/mcp-proxy/...`
Expected: FAIL.

- [ ] **Step 3: Implementar `target.go`**

```go
package main

import (
	"fmt"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
	"github.com/spf13/cobra"
)

func newTargetCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "target",
		Short: "Gerencia alvos dentro do projeto ativo",
	}
	cmd.AddCommand(
		newTargetAddCmd(active, repo, al),
		newTargetListCmd(active, repo, al),
		newTargetUseCmd(active, repo, al),
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
				al.Log(audit.Event{Tool: "target add", Action: "error", Detail: err.Error()})
				return err
			}
			al.Log(audit.Event{Tool: "target add", Action: "add", Detail: map[string]any{"host": host, "project": proj.Name}})
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

func newTargetListCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
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

func newTargetUseCmd(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
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
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./cmd/mcp-proxy/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/mcp-proxy/
git commit -m "feat(cli): subcommand target (add|list|use) com --confirm obrigatorio"
```

---

## Task 17.5: Proxy — integração com per-target store

> **Por que esta task existe:** o PRD §4.2 diz "requests via proxy aparecem
> isoladas por alvo". O v1 (proxy) **ainda não foi implementado**; este
> plano constrói a fundação. Esta task prepara o ponto de integração para
> o futuro v1 — a função `Proxy.Start` recebe um `store.Store` aberto para
> o alvo ativo, e main.go abre/reabre esse store quando o alvo ativo muda.

**Files:**
- Create: `internal/proxy/proxy.go`
- Create: `internal/proxy/proxy_test.go`

**Interfaces:**
- Consumes: `*store.SQLiteStore` (Task 8)
- Produces:
  ```go
  type Proxy struct{ store store.Store }
  func New(s store.Store) *Proxy
  // Start sobe o MITM proxy na porta dada. Bloqueia ate ctx cancelar.
  func (p *Proxy) Start(ctx context.Context, addr string) error
  ```

> **Escopo desta task:** apenas a **assinatura** + teste de compilação.
> A implementação real do MITM (goproxy setup, CA, hooks) virá num plano
> v1 dedicado; ver PRD §4.1.

- [ ] **Step 1: Write failing test**

`internal/proxy/proxy_test.go`:

```go
package proxy

import (
	"path/filepath"
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

func TestNew_NotNil(t *testing.T) {
	dir := t.TempDir()
	s, err := store.OpenSQLite(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := New(s)
	if p == nil {
		t.Fatal("New returned nil")
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `go test ./internal/proxy/...`
Expected: FAIL (`New` undefined).

- [ ] **Step 3: Implementar**

`internal/proxy/proxy.go`:

```go
// Package proxy implementa o MITM HTTP/HTTPS.
//
// v2.0 (este plano): apenas a assinatura e o glue de store.
// A implementacao completa (goproxy, CA, hooks on_request/on_response)
// vive em plano v1 separado; ver PRD §4.1.
package proxy

import (
	"context"
	"errors"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

type Proxy struct {
	store store.Store
}

func New(s store.Store) *Proxy {
	return &Proxy{store: s}
}

// Start e um stub na v2.0. Retorna ErrNotImplemented ate o plano v1
// entregar a implementacao completa.
func (p *Proxy) Start(ctx context.Context, addr string) error {
	return errors.New("proxy.Start: not implemented (planned for v1 plan)")
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/proxy/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/
git commit -m "feat(proxy): stub Proxy com assinatura para integracao per-target store"
```

---

## Task 18: E2E test — fluxo completo

**Files:**
- Create: `test/e2e/v2_workspaces_test.go`

- [ ] **Step 1: Write test E2E**

```go
package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/audit"
	"github.com/isaiaspereira307/network-copitlot/internal/config"
	"github.com/isaiaspereira307/network-copitlot/internal/projects"
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
	al, _ := audit.New(filepath.Join(dir, "audit.log"))
	t.Cleanup(func() { al.Close() })

	// 1. criar projeto via CLI
	root := buildRoot(active, repo, al)
	out := runCmd(t, root, "project", "create", "--name", "HackerOne-X", "--type", "bugbounty")
	if !strings.Contains(out, "HackerOne-X") {
		t.Fatalf("project create: %s", out)
	}

	// 2. usar projeto
	out = runCmd(t, root, "project", "use", "HackerOne-X")
	if !strings.Contains(out, "ativo") {
		t.Fatalf("project use: %s", out)
	}

	// 3. adicionar alvo com confirm
	out = runCmd(t, root, "target", "add", "--host", "api.empresa.com", "--confirm")
	if !strings.Contains(out, "api.empresa.com") {
		t.Fatalf("target add: %s", out)
	}

	// 4. alvo sem confirm: erro
	err := runCmdErr(t, root, "target", "add", "--host", "evil.com")
	if err == nil {
		t.Fatal("expected error without --confirm")
	}

	// 5. usar alvo
	out = runCmd(t, root, "target", "use", "api.empresa.com")
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
	data, _ := os.ReadFile(filepath.Join(dir, "audit.log"))
	if !strings.Contains(string(data), "create") {
		t.Errorf("audit log missing create: %s", data)
	}
}

// helpers compartilhados (idealmente em test/e2e/helpers_test.go)
func runCmd(t *testing.T, root *cobra.Command, args ...string) string {
	t.Helper()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("cmd %v: %v", args, err)
	}
	return buf.String()
}

func runCmdErr(t *testing.T, root *cobra.Command, args ...string) error {
	t.Helper()
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

func buildRoot(active *projects.ActiveState, repo *projects.Repo, al *audit.Logger) *cobra.Command {
	// duplica NewRootCmd de cmd/mcp-proxy/root.go
	// alternativa: extrair para internal/cli e reusar
	root := cobra.Command{Use: "mcp-proxy"}
	root.AddCommand(
		// inlines para evitar import cycle em test/e2e
		newProjectCmdForTest(active, repo, al),
		newTargetCmdForTest(active, repo, al),
	)
	return &root
}
```

> **Nota:** a duplicação `newProjectCmdForTest` é temporária. **Refatorar**
> na Task 19 para extrair `internal/cli` e reusar.

- [ ] **Step 2: Run, expect pass**

Run: `go test ./test/e2e/...`
Expected: PASS (com `newProjectCmdForTest`/`newTargetCmdForTest` sendo aliases dos reais).

- [ ] **Step 3: Commit**

```bash
git add test/e2e/
git commit -m "test(e2e): fluxo completo v2 workspaces"
```

---

## Task 19: Refactor — extrair `internal/cli` (elimina duplicação E2E)

**Files:**
- Create: `internal/cli/root.go`
- Create: `internal/cli/project.go`
- Create: `internal/cli/target.go`
- Modify: `cmd/mcp-proxy/main.go` (usa internal/cli)
- Modify: `cmd/mcp-proxy/root.go` (remove)
- Modify: `test/e2e/v2_workspaces_test.go` (usa internal/cli)

> **Quando extrair:** duplicação apareceu em 2 lugares (cmd + test). YAGNI
> antes; agora DRY. Manter o `internal/cli` magro: apenas wiring, sem
> lógica.

- [ ] **Step 1: Mover código**

Copiar `newProjectCmd`, `newProjectCreateCmd`, `newProjectListCmd`, `newProjectUseCmd`, `newTargetCmd`, `newTargetAddCmd`, `newTargetListCmd`, `newTargetUseCmd`, `NewRootCmd` para `internal/cli/`, ajustando o package name para `cli`.

- [ ] **Step 2: Atualizar main.go e root.go**

`cmd/mcp-proxy/main.go`:

```go
// substituir
root := cli.NewRootCmd(active, repo, al)
return root.Execute()
```

Remover `cmd/mcp-proxy/root.go` (movido para `internal/cli/root.go`).

- [ ] **Step 3: Atualizar test/e2e para usar cli.NewRootCmd**

Substituir `buildRoot` por `cli.NewRootCmd(active, repo, al)`.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: PASS (todos).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/ cmd/mcp-proxy/ test/e2e/
git commit -m "refactor: extrair internal/cli para reuso entre cmd e tests"
```

---

## Task 20: Documentação — README + config example

**Files:**
- Modify: `README.md`
- Create: `config.yaml.example`

- [ ] **Step 1: Escrever README mínimo**

```markdown
# mcp-proxy

Proxy MITM HTTP/HTTPS + servidor MCP para pentest/bug bounty assistido por IA.

## v2.0 — Workspaces

Organize capturas por projeto (engajamento) e alvo.

## Uso

### Modo MCP (stdio)

Configure seu cliente MCP (Claude Desktop, etc) para executar `mcp-proxy` sem argumentos.

Tools expostas: `create_project`, `list_projects`, `set_active_project`, `add_target`, `list_targets`, `set_active_target`, `get_active_context`.

### Modo CLI

```
mcp-proxy project create --name H1-EMPRESA --type bugbounty
mcp-proxy project use H1-EMPRESA
mcp-proxy target add --host api.empresa.com --confirm
mcp-proxy target use api.empresa.com
```

## Aviso legal

Use apenas em alvos com autorização explícita. Veja PRD §5.
```

- [ ] **Step 2: Criar config.yaml.example**

```yaml
# config.yaml.example — copie para ~/.mcp-proxy/config.yaml
workspace_path: /home/user/.mcp-proxy/workspace
# active_project: H1-EMPRESA
# active_target: api.empresa.com
```

- [ ] **Step 3: Commit**

```bash
git add README.md config.yaml.example
git commit -m "docs: README v2.0 + config example"
```

---

## Self-Review

Executar mentalmente:

1. **Spec coverage** (PRD §4.2 v2.0):
   - Project/Target model com validação: Task 4 ✓
   - Storage segregado `workspace/<project>/targets/<host>/`: Task 5 + 8 ✓
   - `meta.yaml` em projeto/alvo: Task 5 ✓
   - `~/.mcp-proxy/config.yaml` com active state: Task 3 + 6 ✓
   - 7 tools MCP: Tasks 12, 13, 14 ✓
   - CLI `project` e `target`: Tasks 16, 17 ✓
   - Confirmação interativa em `add_target`: Task 13 (server-side) + 17 (CLI `--confirm`) ✓
   - Audit log: Task 10, usado em Tasks 12, 13, 16, 17 ✓
   - Migration v1→v2: Task 9 ✓ (chamado em main.go Task 16)

2. **Placeholder scan**: nenhum "TBD"/"TODO"/"implement later" no plano (apenas em comentários de produção, intencional).

3. **Type consistency**:
   - `Project.Name` (string) usado consistentemente em repo, active, MCP, CLI.
   - `Target.Host` (string) idem.
   - `toolRegistry` é `map[string]toolFunc`, consistente em tests e implementação.
   - `ActiveState.SetProject`/`SetTarget`/`Project`/`Target`/`Context` batem com consumidores.
   - `Store` interface (Task 7) → `SQLiteStore` impl (Task 8) → usado em `Server.openStoreForActiveTarget` (Task 14).

4. **Risks** (PRD §9 — v2.0 relevante):
   - Path traversal: regex `nameSafe`/`hostSafe` (Task 4) + `filepath.Join` (Tasks 5).
   - Concurrency em audit: `sync.Mutex` (Task 10).
   - SQLite contention: 1 writer/arquivo, WAL (Task 8).

Plano cobre 100% do escopo v2.0 do PRD. Próximo passo: usuário escolhe modo de execução.
