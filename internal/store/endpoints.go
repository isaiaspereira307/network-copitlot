package store

import (
	"net/url"
	"regexp"
	"strings"
)

// Segmentos dinamicos de path colapsam em {id}, na ordem abaixo. Todas as
// classes mapeiam para o MESMO placeholder — o objetivo e dedup, nao tipagem.
var (
	segNumeric = regexp.MustCompile(`^\d+$`)
	segUUID    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	segHex     = regexp.MustCompile(`(?i)^[0-9a-f]{16,}$`)
	segBase64  = regexp.MustCompile(`^[A-Za-z0-9+/_-]{22,}={0,2}$`)
)

func isDynamicSegment(s string) bool {
	return segNumeric.MatchString(s) || segUUID.MatchString(s) || segHex.MatchString(s) || segBase64.MatchString(s)
}

// normalizePath extrai o path de uma URL e troca segmentos dinamicos por {id}.
// URL invalida/sem path -> devolve a string crua como caiu.
func normalizePath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return rawURL
	}
	segs := strings.Split(u.Path, "/")
	for i, seg := range segs {
		if isDynamicSegment(seg) {
			segs[i] = "{id}"
		}
	}
	return strings.Join(segs, "/")
}

// endpointSampleSize limita sample_ids por endpoint (token-frugal).
// ponytail: teto fixo de 5, subir se o consumidor pedir.
const endpointSampleSize = 5

// ListEndpoints agrega todos os requests por (method, path normalizado).
// A normalizacao do path e Go (regex por segmento) — o grouping roda em Go
// porque o SQL nao enxerga o valor ja normalizado; a intencao do plano
// (GROUP BY normalized_path, method) e preservada na chave de agregacao.
// Rows em id DESC: sample_ids = ids mais recentes do grupo, cap 5.
func (s *SQLiteStore) ListEndpoints() ([]*Endpoint, error) {
	rows, err := s.db.Query(`SELECT id, method, url FROM requests ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := map[string]*Endpoint{}
	order := []string{} // primeira aparicao = grupo mais recente
	for rows.Next() {
		var (
			id       int64
			method   string
			rawURL   string
		)
		if err := rows.Scan(&id, &method, &rawURL); err != nil {
			return nil, err
		}
		path := normalizePath(rawURL)
		key := method + " " + path
		e, ok := groups[key]
		if !ok {
			e = &Endpoint{Method: method, Path: path}
			groups[key] = e
			order = append(order, key)
		}
		e.HitCount++
		if len(e.SampleIDs) < endpointSampleSize {
			e.SampleIDs = append(e.SampleIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Endpoint, 0, len(groups))
	for _, k := range order {
		out = append(out, groups[k])
	}
	return out, nil
}
