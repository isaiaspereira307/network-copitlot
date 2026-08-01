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
