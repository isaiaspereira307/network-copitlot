package proxy

import (
	"net/url"
	"strings"

	"github.com/isaias/network-copitlot/internal/projects"
)

// scope decide se um request HTTP esta dentro do escopo do alvo ativo.
// Regras (PRD §4.1 "Escopo"):
//   1. Sem target ativo: tudo recusado (evita captura fora de escopo).
//   2. Out-of-scope patterns vencem: se casar, recusado.
//   3. In-scope patterns vazias: permite tudo o que sobrou.
//   4. In-scope patterns preenchidas: precisa casar com alguma.
type scope struct {
	target *projects.Target
}

func newScope(t *projects.Target) *scope {
	return &scope{target: t}
}

// matches retorna true se o URL do request esta dentro do escopo.
// Match basico: pattern == host (literal) ou pattern com prefixo "*."
// casa qualquer subdominio. Comparacao case-insensitive.
func (s *scope) matches(u *url.URL) bool {
	if s == nil || s.target == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	// out-of-scope primeiro (vence)
	for _, pat := range s.target.OutOfScopePatterns {
		if hostMatch(pat, host) {
			return false
		}
	}
	// sem in-scope: permite (target existe, sem restricao alem de out-of)
	if len(s.target.InScopePatterns) == 0 {
		return true
	}
	for _, pat := range s.target.InScopePatterns {
		if hostMatch(pat, host) {
			return true
		}
	}
	return false
}

// hostMatch: pattern exato, ou pattern do tipo "*.example.com" casa
// qualquer subdominio. "path" component no pattern e ignorado (escopo
// e por host, nao por path).
func hostMatch(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	// remove path se veio junto (ex: "*.example.com/api")
	if i := strings.IndexAny(pattern, "/?#"); i >= 0 {
		pattern = pattern[:i]
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		return strings.HasSuffix(host, suffix) || host == pattern[2:]
	}
	return host == pattern
}
