package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// DBs criados antes de task 17 nao tem as colunas de skip/truncamento:
	// ALTER idempotente (erro de coluna duplicada e ignorado).
	for _, col := range []struct{ name, ddl string }{
		{"resp_skipped", "INTEGER NOT NULL DEFAULT 0"},
		{"resp_truncated", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if _, err := db.Exec("ALTER TABLE requests ADD COLUMN " + col.name + " " + col.ddl); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			db.Close()
			return nil, fmt.Errorf("migrate column %s: %w", col.name, err)
		}
	}
	if _, err := db.Exec(findingSchemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply findings schema: %w", err)
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
		INSERT INTO requests (ts, method, url, req_headers, req_body, status, resp_headers, resp_body, resp_len, resp_skipped, resp_truncated, ttfb_ms, tags, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.Ts, r.Method, r.URL, headersReq, r.ReqBody, r.Status, headersResp, r.RespBody, r.RespLen, r.RespBodySkipped, r.RespBodyTruncated, r.TTFBms, tags, r.Notes)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) Get(id int64) (*Request, error) {
	row := s.db.QueryRow(`SELECT id, ts, method, url, req_headers, req_body, status, resp_headers, resp_body, resp_len, resp_skipped, resp_truncated, ttfb_ms, tags, notes FROM requests WHERE id = ?`, id)
	return scanRequest(row)
}

func (s *SQLiteStore) List(f ListFilter) ([]*RequestSummary, error) {
	q := `SELECT id, ts, method, url, status, resp_len FROM requests WHERE 1=1`
	var args []any
	if f.MethodFilter != "" {
		q += ` AND method = ?`
		args = append(args, f.MethodFilter)
	}
	if f.StatusFilter != 0 {
		q += ` AND status = ?`
		args = append(args, f.StatusFilter)
	}
	if f.HostFilter != "" {
		q += ` AND url LIKE ?`
		args = append(args, "%"+f.HostFilter+"%")
	}
	if f.PathContains != "" {
		q += ` AND url LIKE ?`
		args = append(args, "%"+f.PathContains+"%")
	}
	if f.SinceID > 0 {
		q += ` AND id > ?`
		args = append(args, f.SinceID)
	}
	q += ` ORDER BY id DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	if f.Offset > 0 {
		q += ` OFFSET ?`
		args = append(args, f.Offset)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RequestSummary
	for rows.Next() {
		var sm RequestSummary
		if err := rows.Scan(&sm.ID, &sm.Ts, &sm.Method, &sm.URL, &sm.Status, &sm.RespLen); err != nil {
			return nil, err
		}
		out = append(out, &sm)
	}
	return out, rows.Err()
}

// All faz stream de todos os requests com corpos (req+resp). Filtros de List
// nao se aplicam aqui; usado pelo scanner/export.
func (s *SQLiteStore) All() ([]*Request, error) {
	rows, err := s.db.Query(`SELECT id, ts, method, url, req_headers, req_body, status, resp_headers, resp_body, resp_len, resp_skipped, resp_truncated, ttfb_ms, tags, notes FROM requests ORDER BY id DESC`)
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

func (s *SQLiteStore) GetDetail(id int64, include string, maxBody int, bodyRange string) (*RequestDetail, error) {
	r, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	d := &RequestDetail{
		ID:      r.ID,
		Ts:      r.Ts,
		Method:  r.Method,
		URL:     r.URL,
		Status:  r.Status,
		RespLen: r.RespLen,
	}
	switch include {
	case "all":
		d.ReqHeaders = r.ReqHeaders
		d.RespHeaders = r.RespHeaders
		d.ReqBody, d.ReqTotalLen, d.ReqBodyTruncated = cutBody(r.ReqBody, maxBody, bodyRange)
		d.RespBody, d.RespTotalLen, d.RespBodyTruncated = cutBody(r.RespBody, maxBody, bodyRange)
	case "body":
		d.ReqBody, d.ReqTotalLen, d.ReqBodyTruncated = cutBody(r.ReqBody, maxBody, bodyRange)
		d.RespBody, d.RespTotalLen, d.RespBodyTruncated = cutBody(r.RespBody, maxBody, bodyRange)
	default: // headers
		d.ReqHeaders = r.ReqHeaders
		d.RespHeaders = r.RespHeaders
	}
	return d, nil
}

// cutBody aplica o orcamento maxBody e depois a janela bodyRange ("start-end")
// sobre o corpo ja limitado. TotalLen sempre reflete o tamanho original.
func cutBody(body []byte, maxBody int, rng string) (out []byte, total int, truncated bool) {
	if maxBody <= 0 {
		maxBody = 8192
	}
	total = len(body)
	if total > maxBody {
		body = body[:maxBody]
		truncated = true
	}
	out = body
	if rng == "" {
		return
	}
	parts := strings.SplitN(rng, "-", 2)
	if len(parts) != 2 {
		return
	}
	start, err1 := strconv.Atoi(parts[0])
	end, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return
	}
	if start < 0 {
		start = 0
	}
	if end > len(body) {
		end = len(body)
	}
	if start >= end {
		out = nil
		return
	}
	out = body[start:end]
	return
}

// searchScanLimit cap internal de linhas varridas; limit do chamador e o teto de
// retorno. ponytail: limite fixo de 500, usar paginacao SQL se datasets crescerem.
const searchScanLimit = 500

// snippetWindow e o raio (em chars) do trecho ao redor do primeiro match.
const snippetWindow = 80

// matcher casa `pattern` em um body: se compila como regex, usa regex; senao
// cai para substring case-sensitive via bytes.Contains/Index.
type matcher struct {
	re  *regexp.Regexp // nil = modo substring
	str string
}

func newMatcher(pattern string) *matcher {
	m := &matcher{str: pattern}
	if re, err := regexp.Compile(pattern); err == nil {
		m.re = re
	}
	return m
}

// find devolve o indice do primeiro match em body, ou -1.
func (m *matcher) find(body []byte) int {
	if m.re != nil {
		if loc := m.re.FindIndex(body); loc != nil {
			return loc[0]
		}
		return -1
	}
	return bytes.Index(body, []byte(m.str))
}

// makeSnippet extrai body[max(0,idx-window):min(len,idx+window)].
func makeSnippet(body []byte, idx int) string {
	lo := idx - snippetWindow
	if lo < 0 {
		lo = 0
	}
	hi := idx + snippetWindow
	if hi > len(body) {
		hi = len(body)
	}
	return string(body[lo:hi])
}

// SearchBodies procura `pattern` em req_body/resp_body conforme `scope`
// ("req"|"resp"|"both", default "both"). Pattern que compila como regex vira
// regex; senao e substring case-sensitive. Retorna um BodyMatch por request em
// ordem id DESC (consistente com List), um snippet +/-80 chars por hit, e
// respeita `limit` (default 30) como teto de retorno.
func (s *SQLiteStore) SearchBodies(pattern string, scope string, limit int) ([]*BodyMatch, error) {
	if pattern == "" {
		return nil, errors.New("SearchBodies: pattern vazio")
	}
	if scope == "" {
		scope = "both"
	}
	switch scope {
	case "req", "resp", "both":
	default:
		return nil, fmt.Errorf("SearchBodies: scope invalido %q", scope)
	}
	if limit <= 0 {
		limit = 30
	}
	m := newMatcher(pattern)

	rows, err := s.db.Query(`SELECT id, url, req_body, resp_body FROM requests ORDER BY id DESC LIMIT ?`, searchScanLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BodyMatch
	for rows.Next() {
		var (
			id       int64
			url      string
			reqBody  []byte
			respBody []byte
		)
		if err := rows.Scan(&id, &url, &reqBody, &respBody); err != nil {
			return nil, err
		}
		var (
			body []byte
			idx  = -1
		)
		switch scope {
		case "req":
			idx = m.find(reqBody)
			body = reqBody
		case "resp":
			idx = m.find(respBody)
			body = respBody
		default: // both: req tem prioridade no snippet
			idx = m.find(reqBody)
			body = reqBody
			if idx < 0 {
				idx = m.find(respBody)
				body = respBody
			}
		}
		if idx >= 0 {
			out = append(out, &BodyMatch{ID: id, URL: url, MatchSnippet: makeSnippet(body, idx)})
			if len(out) >= limit {
				break
			}
		}
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

// SetRequestTags substitui as tags de um request (Logger++ v5.0).
func (s *SQLiteStore) SetRequestTags(id int64, tags []string) error {
	j, err := marshalJSON(tags)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE requests SET tags = ? WHERE id = ?`, j, id)
	return err
}

// AddRequestNote adiciona um comentario (anexo com TS) ao campo notes.
func (s *SQLiteStore) AddRequestNote(id int64, note string) error {
	r, err := s.Get(id)
	if err != nil {
		return err
	}
	existing := r.Notes
	if existing != "" {
		existing += "\n"
	}
	existing += fmt.Sprintf("[%d] %s", time.Now().UnixMilli(), note)
	_, err = s.db.Exec(`UPDATE requests SET notes = ? WHERE id = ?`, existing, id)
	return err
}

// ListTags devolve todas as tags em uso (deduplicadas).
func (s *SQLiteStore) ListTags() ([]string, error) {
	rows, err := s.db.Query(`SELECT tags FROM requests WHERE tags IS NOT NULL AND tags != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return nil, err
		}
		if j == "" {
			continue
		}
		var tags []string
		if err := json.Unmarshal([]byte(j), &tags); err != nil {
			continue
		}
		for _, t := range tags {
			if t != "" {
				set[t] = true
			}
		}
	}
	var out []string
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanRequest(s scanner) (*Request, error) {
	var (
		r         Request
		hReq      string
		hResp     string
		tagsJSON  string
		skipped   int
		truncated int
	)
	err := s.Scan(&r.ID, &r.Ts, &r.Method, &r.URL, &hReq, &r.ReqBody, &r.Status, &hResp, &r.RespBody, &r.RespLen, &skipped, &truncated, &r.TTFBms, &tagsJSON, &r.Notes)
	if err != nil {
		return nil, err
	}
	r.RespBodySkipped = skipped != 0
	r.RespBodyTruncated = truncated != 0
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
