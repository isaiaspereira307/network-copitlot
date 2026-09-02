package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// FindingStatus / FindingType sao enums do scanner passivo (PRD v4.0 / v5.1).
type FindingStatus string

const (
	StatusOpen         FindingStatus = "open"
	StatusTriaged      FindingStatus = "triaged"
	StatusConfirmed    FindingStatus = "confirmed"
	StatusFalsePositive FindingStatus = "false-positive"
	StatusClosed       FindingStatus = "closed"
)

// FindingSeverity: info | low | med | high | crit.
type FindingSeverity string

const (
	SevInfo  FindingSeverity = "info"
	SevLow   FindingSeverity = "low"
	SevMed   FindingSeverity = "med"
	SevHigh  FindingSeverity = "high"
	SevCrit  FindingSeverity = "crit"
)

// Finding e uma deteccao estruturada com evidences (JSON) e ciclo de vida.
type Finding struct {
	ID         int64           `json:"id"`
	Ts         int64           `json:"ts"`     // unix ms
	Type       string          `json:"type"`   // XSS | IDOR | SQLi | SSRF | redirect | secret | other
	Severity   FindingSeverity `json:"severity"`
	URL        string          `json:"url"`
	RequestID  int64           `json:"request_id"`
	Evidence   string          `json:"evidence"` // JSON string
	Status     FindingStatus   `json:"status"`
	Notes      string          `json:"notes"`
}

// findingSchemaSQL define a tabela `findings` (PRD Anexo B).
const findingSchemaSQL = `
CREATE TABLE IF NOT EXISTS findings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  type TEXT NOT NULL,
  severity TEXT NOT NULL,
  url TEXT NOT NULL,
  request_id INTEGER,
  evidence TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open',
  notes TEXT
);
CREATE INDEX IF NOT EXISTS idx_findings_ts ON findings(ts);
CREATE INDEX IF NOT EXISTS idx_findings_status_severity ON findings(status, severity);
`

// EnsureFindingsSchema garante a existencia da tabela findings (idempotente).
func (s *SQLiteStore) EnsureFindingsSchema() error {
	_, err := s.db.Exec(findingSchemaSQL)
	return err
}

// AddFinding insere um finding e devolve o id.
func (s *SQLiteStore) AddFinding(f *Finding) (int64, error) {
	if f.Status == "" {
		f.Status = StatusOpen
	}
	if f.Ts == 0 {
		f.Ts = time.Now().UnixMilli()
	}
	res, err := s.db.Exec(`
		INSERT INTO findings (ts, type, severity, url, request_id, evidence, status, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, f.Ts, f.Type, f.Severity, f.URL, f.RequestID, f.Evidence, f.Status, f.Notes)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListFindings retorna findings filtrados por type/severity, ordenados por
// severidade (crit > info) e depois recencia.
func (s *SQLiteStore) ListFindings(typeFilter, severity string) ([]*Finding, error) {
	sevRank := fmt.Sprintf(`CASE severity WHEN 'crit' THEN 0 WHEN 'high' THEN 1 WHEN 'med' THEN 2 WHEN 'low' THEN 3 ELSE 4 END`)
	q := `SELECT id, ts, type, severity, url, request_id, evidence, status, notes FROM findings WHERE 1=1`
	var args []any
	if typeFilter != "" {
		q += ` AND type = ?`
		args = append(args, typeFilter)
	}
	if severity != "" {
		q += ` AND severity = ?`
		args = append(args, severity)
	}
	q += fmt.Sprintf(` ORDER BY %s ASC, ts DESC`, sevRank)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Finding
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFinding retorna um finding por id.
func (s *SQLiteStore) GetFinding(id int64) (*Finding, error) {
	row := s.db.QueryRow(`SELECT id, ts, type, severity, url, request_id, evidence, status, notes FROM findings WHERE id = ?`, id)
	return scanFinding(row)
}

// SetFindingStatus atualiza o status de um finding (ciclo de vida v5.1).
func (s *SQLiteStore) SetFindingStatus(id int64, status FindingStatus) error {
	res, err := s.db.Exec(`UPDATE findings SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("finding %d nao encontrado", id)
	}
	return nil
}

// AddFindingNote appends uma nota ao finding.
func (s *SQLiteStore) AddFindingNote(id int64, note string) error {
	f, err := s.GetFinding(id)
	if err != nil {
		return err
	}
	notes := f.Notes
	if notes != "" {
		notes += "\n"
	}
	notes += note
	_, err = s.db.Exec(`UPDATE findings SET notes = ? WHERE id = ?`, notes, id)
	return err
}

func scanFinding(row interface{ Scan(...any) error }) (*Finding, error) {
	var f Finding
	err := row.Scan(&f.ID, &f.Ts, &f.Type, &f.Severity, &f.URL, &f.RequestID, &f.Evidence, &f.Status, &f.Notes)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, err
	}
	return &f, nil
}

// EvidenceJSON serializa um mapa de evidencias para a coluna evidence.
func EvidenceJSON(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// ParseEvidence deserializa o campo evidence JSON.
func ParseEvidence(s string) map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

// FindingStatusValid valida um status do ciclo de vida.
func FindingStatusValid(s string) bool {
	switch FindingStatus(s) {
	case StatusOpen, StatusTriaged, StatusConfirmed, StatusFalsePositive, StatusClosed:
		return true
	}
	return false
}
