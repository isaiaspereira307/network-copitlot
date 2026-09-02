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
	if p.Name == "." || p.Name == ".." {
		return fmt.Errorf("name invalido (path traversal): %q", p.Name)
	}
	if !p.Type.Valid() {
		return fmt.Errorf("type invalido: %q (use bugbounty|pentest)", p.Type)
	}
	return nil
}

// Dir assumes the receiver has been validated; callers must call Validate() first.
func (p *Project) Dir(workspace string) string {
	return filepath.Join(workspace, p.Name)
}

type Target struct {
	Host               string             `yaml:"host"`
	InScopePatterns    []string           `yaml:"in_scope"`
	OutOfScopePatterns []string           `yaml:"out_of_scope"`
	MatchReplaceRules  []MatchReplaceRule `yaml:"match_replace,omitempty"`
	Notes              string             `yaml:"notes"`
	CreatedAt          time.Time          `yaml:"created_at"`
}

// MatchReplaceRule reescreve requests in-scope em tráfego vivo no proxy antes de
// encaminhar ao upstream. Match e regex (RE2); Replace usa a sintaxe de
// ReplaceAll ($1, ${nome}). Part escolhe onde aplicar.
type MatchReplaceRule struct {
	Name    string `yaml:"name" json:"name"`
	Part    string `yaml:"part" json:"part"`   // url | req_header | req_body
	Match   string `yaml:"match" json:"match"` // regex
	Replace string `yaml:"replace" json:"replace"`
	Header  string `yaml:"header,omitempty" json:"header,omitempty"` // nome do header quando part=req_header
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// validParts sao os alvos aceitos de uma MatchReplaceRule.
var validParts = map[string]bool{"url": true, "req_header": true, "req_body": true}

func (t *Target) Validate() error {
	if t.Host == "" {
		return errors.New("host obrigatorio")
	}
	if !hostSafe.MatchString(t.Host) {
		return fmt.Errorf("host invalido: %q", t.Host)
	}
	if t.Host == "." || t.Host == ".." {
		return fmt.Errorf("host invalido (path traversal): %q", t.Host)
	}
	for i, r := range t.MatchReplaceRules {
		if !validParts[r.Part] {
			return fmt.Errorf("match_replace[%d]: part invalido %q (use url|req_header|req_body)", i, r.Part)
		}
		if r.Match == "" {
			return fmt.Errorf("match_replace[%d]: match (regex) obrigatorio", i)
		}
		if _, err := regexp.Compile(r.Match); err != nil {
			return fmt.Errorf("match_replace[%d]: regex invalida %q: %w", i, r.Match, err)
		}
		if r.Part == "req_header" && r.Header == "" {
			return fmt.Errorf("match_replace[%d]: part=req_header exige header", i)
		}
	}
	return nil
}

// Dir assumes the receiver has been validated; callers must call Validate() first.
func (t *Target) Dir(projectDir string) string {
	return filepath.Join(projectDir, "targets", t.Host)
}
