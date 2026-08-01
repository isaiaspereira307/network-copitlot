package projects

import (
	"fmt"

	"github.com/isaias/network-copitlot/internal/config"
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
