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
