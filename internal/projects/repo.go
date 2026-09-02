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
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("projeto nao encontrado: %s", name)
		}
		return nil, err
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
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("alvo nao encontrado: %s/%s", projectName, host)
		}
		return nil, err
	}
	var t Target
	if err := readYAML(meta, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) UpdateTarget(projectName string, t *Target) error {
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
	if !exists {
		return fmt.Errorf("alvo nao encontrado: %s/%s", projectName, t.Host)
	}
	if err := t.Validate(); err != nil {
		return err
	}
	return writeYAML(filepath.Join(r.targetDir(projectName, t.Host), "meta.yaml"), t)
}

func (r *Repo) SetScope(projectName, host string, inScope, outOfScope []string) error {
	t, err := r.LoadTarget(projectName, host)
	if err != nil {
		return err
	}
	t.InScopePatterns = inScope
	t.OutOfScopePatterns = outOfScope
	return r.UpdateTarget(projectName, t)
}

// SetMatchReplace persiste as regras de match/replace do alvo (substitui a
// lista inteira). UpdateTarget valida cada regra antes de gravar; o proxy
// recarrega vivo via mtime do meta.yaml.
func (r *Repo) SetMatchReplace(projectName, host string, rules []MatchReplaceRule) error {
	t, err := r.LoadTarget(projectName, host)
	if err != nil {
		return err
	}
	t.MatchReplaceRules = rules
	return r.UpdateTarget(projectName, t)
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
