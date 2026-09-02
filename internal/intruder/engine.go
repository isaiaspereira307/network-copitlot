package intruder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JobStatus enumera os estados de uma execucao intruder.
type JobStatus string

const (
	StatusQueued  JobStatus = "queued"
	StatusRunning JobStatus = "running"
	StatusDone    JobStatus = "done"
	StatusCancel  JobStatus = "cancelled"
	StatusError   JobStatus = "error"
)

// CaseResult e o resultado individual de um caso executado.
type CaseResult struct {
	Case     int    `json:"case"`      // indice do caso no genero
	ReplayID int64  `json:"replay_id"` // id do novo request persistido
	Status   int    `json:"status"`
	RespLen  int    `json:"resp_len"`
	Err      string `json:"err,omitempty"`
	TimeMs   int64  `json:"time_ms"`
}

// Job e o estado completo de uma execucao intruder, persistido em
// intruder/jobs/<id>/results.json com o schema desenhado no PRD v3.0.
type Job struct {
	ID          string       `json:"id"`
	Attack      AttackType   `json:"attack"`
	Positions   []string     `json:"positions"`
	BaseID      int64        `json:"base_id"`
	Status      JobStatus    `json:"status"`
	TotalCases  int          `json:"total_cases"`
	Done        int          `json:"done"`
	Anomalies   int          `json:"anomalies"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  *time.Time   `json:"finished_at,omitempty"`
	Error       string       `json:"error,omitempty"`
	Results     []CaseResult `json:"results,omitempty"`
	AnomResults []CaseResult `json:"anomalous_results,omitempty"`
}

// ReplayFunc executa um caso do intruder: injeta os payloads no request base e
// reenvia sob scope guard, persistindo o novo request. Retorna o resumo.
// Implementada no mcpserver (tem acesso ao store e scope), aqui apenas contrato.
type ReplayFunc func(caseIdx int, payloads []string) (CaseResult, error)

// Engine coordena a execucao de um Job: lança um worker pool com throttle e
// cancelamento, persistindo o progresso em resultados.json. Todo acesso ao
// estado do job e serializado sob um mutex; callers leem via Snapshot/Get.
type Engine struct {
	mu   sync.Mutex
	jobs map[string]*Job
	dir  string
}

func NewEngine(resultsDir string) *Engine {
	return &Engine{jobs: map[string]*Job{}, dir: resultsDir}
}

// Run executa o job de forma assincrona (goroutine) e retorna a SNAPSHOT
// inicial (imutavel). replay deve honrar o scope guard.
func (e *Engine) Run(jobID string, attack AttackType, positions []string, baseID int64,
	cases []Case, throttleRPS float64, replay ReplayFunc) Job {

	job := &Job{
		ID:         jobID,
		Attack:     attack,
		Positions:  positions,
		BaseID:     baseID,
		Status:     StatusQueued,
		TotalCases: len(cases),
		StartedAt:  time.Now().UTC(),
	}
	e.mu.Lock()
	e.jobs[jobID] = job
	e.mu.Unlock()

	go e.run(job, cases, throttleRPS, replay)

	// retorna a snapshot inicial sob lock (a goroutine pode ja ter mutado o job)
	e.mu.Lock()
	snap := e.snapshotLocked(job)
	e.mu.Unlock()
	return snap
}

func (e *Engine) run(job *Job, cases []Case, throttleRPS float64, replay ReplayFunc) {
	e.update(job, func(j *Job) { j.Status = StatusRunning })

	var interval time.Duration
	if throttleRPS > 0 {
		interval = time.Second / time.Duration(throttleRPS)
	}

	for i, c := range cases {
		var cancelled bool
		e.mu.Lock()
		cancelled = job.Status == StatusCancel
		e.mu.Unlock()
		if cancelled {
			e.finish(job, StatusCancel, "cancelled by user")
			return
		}
		start := time.Now()
		r, err := replay(i, c.Payloads)
		if err != nil {
			r.Err = err.Error()
		}
		r.TimeMs = time.Since(start).Milliseconds()

		e.mu.Lock()
		job.Done++
		job.Results = append(job.Results, r)
		if r.Err != "" || isAnomalousResult(r) {
			job.Anomalies++
			job.AnomResults = append(job.AnomResults, r)
		}
		snap := e.snapshotLocked(job)
		results := append([]CaseResult(nil), job.Results...)
		e.mu.Unlock()
		e.persist(snap)
		_ = results

		if interval > 0 {
			time.Sleep(interval)
		}
	}
	e.finish(job, StatusDone, "")
}

func isAnomalousResult(r CaseResult) bool {
	return r.Err != ""
}

// update aplica fn ao job sob lock.
func (e *Engine) update(job *Job, fn func(*Job)) {
	e.mu.Lock()
	fn(job)
	e.mu.Unlock()
}

func (e *Engine) finish(job *Job, s JobStatus, errMsg string) {
	now := time.Now()
	e.mu.Lock()
	job.Status = s
	job.FinishedAt = &now
	if errMsg != "" {
		job.Error = errMsg
	}
	e.mu.Unlock()
}

// snapshotLocked devolve uma copia do job — chamar com e.mu LOCKED (ou copiar).
func (e *Engine) snapshotLocked(job *Job) Job {
	snap := *job
	snap.Results = append([]CaseResult(nil), job.Results...)
	snap.AnomResults = append([]CaseResult(nil), job.AnomResults...)
	if job.FinishedAt != nil {
		f := *job.FinishedAt
		snap.FinishedAt = &f
	}
	return snap
}

// Snapshot retorna uma copia imutavel do estado de um job.
func (e *Engine) Snapshot(id string) (Job, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	job, ok := e.jobs[id]
	if !ok {
		return Job{}, false
	}
	return e.snapshotLocked(job), true
}

// Cancel interrompe um job em andamento.
func (e *Engine) Cancel(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	job, ok := e.jobs[id]
	if !ok {
		return false
	}
	if job.Status == StatusRunning || job.Status == StatusQueued {
		job.Status = StatusCancel
	}
	return true
}

// persist grava o snapshot do job em disco (resultados.json por job).
func (e *Engine) persist(snap Job) {
	if e.dir == "" {
		return
	}
	dir := filepath.Join(e.dir, "jobs", snap.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	b, _ := json.MarshalIndent(snap, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "results.json"), b, 0o600)
}

// Get é mantido por compatibilidade, devolvendo um snapshot. Callers devem usar
// Snapshot; Get retorna erro se o job não existe.
func (e *Engine) Get(id string) (Job, error) {
	snap, ok := e.Snapshot(id)
	if !ok {
		return Job{}, fmt.Errorf("job %s nao encontrado", id)
	}
	return snap, nil
}
