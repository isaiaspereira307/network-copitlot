package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/isaiaspereira307/network-copitlot/internal/projects"
)

// applyMatchReplace reescreve um request in-scope conforme as regras habilitadas,
// antes de encaminhar ao upstream. Regras de url que empurrariam o host para
// fora do escopo sao revertidas (defense-in-depth: match/replace nunca vaza
// trafego para fora do alvo autorizado). scopeMatch valida o host pos-rewrite.
//
// ponytail: regexp.Compile por regra por request. Proxy opera em velocidade de
// navegacao humana; cache de regex so se um caso real de alto volume aparecer.
func applyMatchReplace(req *http.Request, rules []projects.MatchReplaceRule, scopeMatch func(*url.URL) bool, logger *log.Logger) {
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		re, err := regexp.Compile(r.Match)
		if err != nil {
			if logger != nil {
				logger.Printf("match_replace %q: regex invalida, pulando: %v", r.Name, err)
			}
			continue
		}
		switch r.Part {
		case "url":
			old := req.URL.String()
			rewritten := re.ReplaceAllString(old, r.Replace)
			if rewritten == old {
				continue
			}
			u, err := url.Parse(rewritten)
			if err != nil || u.Host == "" {
				if logger != nil {
					logger.Printf("match_replace %q: url reescrita invalida %q, pulando", r.Name, rewritten)
				}
				continue
			}
			if scopeMatch != nil && !scopeMatch(u) {
				if logger != nil {
					logger.Printf("match_replace %q: url reescrita sairia do escopo (%s), revertida", r.Name, u.Host)
				}
				continue
			}
			req.URL = u
			req.Host = u.Host
		case "req_header":
			for i, v := range req.Header[http.CanonicalHeaderKey(r.Header)] {
				req.Header[http.CanonicalHeaderKey(r.Header)][i] = re.ReplaceAllString(v, r.Replace)
			}
		case "req_body":
			if req.Body == nil {
				continue
			}
			b, err := io.ReadAll(req.Body)
			req.Body.Close()
			if err != nil {
				req.Body = io.NopCloser(bytes.NewReader(nil))
				continue
			}
			nb := re.ReplaceAll(b, []byte(r.Replace))
			req.Body = io.NopCloser(bytes.NewReader(nb))
			req.ContentLength = int64(len(nb))
			req.Header.Set("Content-Length", strconv.Itoa(len(nb)))
		}
	}
}
