// Package macro implements v3.0 session/macro handling: a chain of requests
// (login + csrf + replay) recorded as a "macro" that keeps a session alive, with
// variables extracted from responses via regex and substituted into later steps.
package macro

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Step e um request individual dentro de uma macro.
type Step struct {
	Method    string              `json:"method"`
	URL       string              `json:"url"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      string              `json:"body,omitempty"`
	// Extractors: regex a rodar sobre a resposta para gravar variaveis de sessao.
	Extractors []Extractor `json:"extractors,omitempty"`
}

// Extractor captura uma variavel da resposta (para usar em steps seguintes).
type Extractor struct {
	// Name da variavel (referenciada como {name} nos steps posteriores).
	Name string `json:"name"`
	// Pattern regex (RE2) com 1 grupo de captura.
	Pattern string `json:"pattern"`
}

// Macro e uma cadeia nomeada de requests.
type Macro struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Steps     []Step    `json:"steps"`
	CreatedAt time.Time `json:"created_at"`
}

// PlayResult resume a execucao de uma macro.
type PlayResult struct {
	MacroID    string            `json:"macro_id"`
	StepsRun   int               `json:"steps_run"`
	Status     int               `json:"last_status"`
	Vars       map[string]string `json:"vars,omitempty"` // variaveis extraidas
	Err        string            `json:"err,omitempty"`
}

// Session registra uma execucao ativa de macro mantendo as variaveis extraidas.
type Session struct {
	ID        string            `json:"id"`
	MacroID   string            `json:"macro_id"`
	Name      string            `json:"name"`
	Vars      map[string]string `json:"vars"`
	CreatedAt time.Time         `json:"created_at"`
	LastRun   time.Time         `json:"last_run"`
}

// StepRunner executa um Step de macro e devolve o body da resposta (para os
// extractors) e o status. Implementada no mcpserver (tem store/scope).
type StepRunner func(step Step, vars map[string]string) (body []byte, status int, err error)

// Manager persiste macros e gerencia sessoes em disco: macro/<name>.json.
type Manager struct {
	dir string
	mu  sync.Mutex
	sessions map[string]*Session
}

func NewManager(dir string) *Manager {
	return &Manager{dir: dir, sessions: map[string]*Session{}}
}

var varSafe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var nameSafe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// Save grava uma macro em disco.
func (m *Manager) Save(mac *Macro) error {
	if mac.Name == "" {
		return errors.New("name obrigatorio")
	}
	if !nameSafe.MatchString(mac.Name) {
		return fmt.Errorf("name invalido: %q", mac.Name)
	}
	if mac.ID == "" {
		mac.ID = newID()
	}
	if mac.CreatedAt.IsZero() {
		mac.CreatedAt = time.Now().UTC()
	}
	dir := filepath.Join(m.dir, "macros")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(mac, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, mac.Name+".json"), b, 0o600)
}

// Load le uma macro por nome.
func (m *Manager) Load(name string) (*Macro, error) {
	dir := filepath.Join(m.dir, "macros")
	b, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("macro %q nao encontrada", name)
		}
		return nil, err
	}
	var mac Macro
	if err := json.Unmarshal(b, &mac); err != nil {
		return nil, err
	}
	return &mac, nil
}

// List retorna os nomes das macros salvas.
func (m *Manager) List() ([]string, error) {
	dir := filepath.Join(m.dir, "macros")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, trimExt(e.Name()))
		}
	}
	return names, nil
}

func trimExt(n string) string {
	return n[:len(n)-len(filepath.Ext(n))]
}

// Start abre uma sessao com base numa macro (não executa; apenas registra).
func (m *Manager) Start(mac *Macro) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &Session{
		ID:        "sess_" + newID(),
		MacroID:   mac.ID,
		Name:      mac.Name,
		Vars:      map[string]string{},
		CreatedAt: time.Now().UTC(),
	}
	m.sessions[s.ID] = s
	return s
}

// Play executa uma macro por nome, mantendo/retornando as variaveis extraidas.
// Se sessionID for dado, reutiliza/atualiza essa sessao; senao cria uma nova.
func (m *Manager) Play(name string, sessionID string, runner StepRunner) (*PlayResult, error) {
	mac, err := m.Load(name)
	if err != nil {
		return nil, err
	}
	var sess *Session
	m.mu.Lock()
	if sessionID != "" {
		sess = m.sessions[sessionID]
	}
	if sess == nil || sess.MacroID != mac.ID {
		sess = &Session{ID: "sess_" + newID(), MacroID: mac.ID, Name: mac.Name, Vars: map[string]string{}, CreatedAt: time.Now().UTC()}
	}
	sess.Vars = cloneVars(sess.Vars)
	m.sessions[sess.ID] = sess
	m.mu.Unlock()

	res := &PlayResult{MacroID: mac.ID, Vars: map[string]string{}}
	for i, step := range mac.Steps {
		body, status, rErr := runner(step, sess.Vars)
		if rErr != nil {
			res.Err = fmt.Errorf("step %d: %w", i, rErr).Error()
			res.StepsRun = i
			return res, rErr
		}
		res.StepsRun = i + 1
		res.Status = status
		for _, ex := range step.Extractors {
			if v, ok := extractVar(body, ex); ok {
				sess.Vars[ex.Name] = v
				res.Vars[ex.Name] = v
			}
		}
	}
	sess.LastRun = time.Now().UTC()
	return res, nil
}

// SessionByID retorna uma sessao ativa.
func (m *Manager) SessionByID(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Vars retorna as variaveis de uma sessao.
func (m *Manager) Vars(sessionID string) map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[sessionID]; ok {
		return cloneVars(s.Vars)
	}
	return map[string]string{}
}

// Substitute aplica as variaveis num step (format {name}), deixando intactos os
// nomes desconhecidos.
func Substitute(s string, vars map[string]string) string {
	return varSubRe.ReplaceAllStringFunc(s, func(match string) string {
		name := match[1 : len(match)-1]
		if v, ok := vars[name]; ok {
			return v
		}
		return match
	})
}

var varSubRe = regexp.MustCompile(`\{([A-Za-z0-9_.-]+)\}`)

func extractVar(body []byte, ex Extractor) (string, bool) {
	if ex.Pattern == "" || ex.Name == "" || !varSafe.MatchString(ex.Name) {
		return "", false
	}
	re, err := regexp.Compile(ex.Pattern)
	if err != nil {
		return "", false
	}
	m := re.FindSubmatch(body)
	if len(m) < 2 {
		return "", false
	}
	return string(m[1]), true
}

func cloneVars(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func newID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
