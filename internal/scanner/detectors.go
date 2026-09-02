// Package scanner implements v4.0 passive detection: heuristics over ALREADY
// captured traffic (no new payloads sent) plus a passive sitemap. Safe by
// design: it only reads what the proxy recorded and never touches the network.
package scanner

import (
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// Detection e o resultado de um detector sobre um request capturado.
type Detection struct {
	Type     string // XSS | IDOR | SQLi | SSRF | redirect | secret
	Severity store.FindingSeverity
	Evidence map[string]any
}

// runDetectors aplica todos os detectores a um request e agrega os achados.
func runDetectors(r *store.Request, scopeHost string) []Detection {
	var dets []Detection
	appendIf := func(d Detection) { dets = append(dets, d) }

	// SSRF + secrets precisam analisar resp_body; refletido XSS usa req+resp.
	respBody := string(r.RespBody)
	reqBody := string(r.ReqBody)

	// ---- Reflected XSS: parametro de query aparece verbatim na resposta ----
	u, err := url.Parse(r.URL)
	if err == nil && u.RawQuery != "" && len(respBody) > 0 && len(respBody) < 1<<20 {
		for _, v := range u.Query() {
			for _, val := range v {
				if strings.ContainsAny(val, "<>\"'") {
					// payloads de markup podem ser longos; nao truncar por tamanho
				} else if len(val) > 40 || len(val) < 3 {
					continue
				}
				if strings.Contains(respBody, val) && looksLikeMarkup(val) {
					appendIf(Detection{
						Type: "XSS", Severity: store.SevMed,
						Evidence: map[string]any{
							"param": val, "request_id": r.ID, "url": r.URL,
							"note": "query value refletido verbatim no corpo; markup detectado — possivel XSS refletido",
						},
					})
					break
				}
			}
		}
	}

	// ---- SQLi: error patterns no corpo da resposta ----
	if sqli := matchAny(respBody, sqliPatterns); sqli != "" {
		appendIf(Detection{
			Type: "SQLi", Severity: store.SevHigh,
			Evidence: map[string]any{"request_id": r.ID, "url": r.URL, "snippet": sqli, "note": "padrao de erro SQL na resposta"},
		})
	}

	// ---- SSRF hints: request com param de URL + resposta com dados de rede ----
	if hasURLLikeParam(reqBody, r.URL) && (containsPrivateIP(respBody) || containsMetadataIP(respBody)) {
		appendIf(Detection{
			Type: "SSRF", Severity: store.SevHigh,
			Evidence: map[string]any{"request_id": r.ID, "url": r.URL, "note": "param aceita URL e resposta contem IP interno/metadata — possivel SSRF"},
		})
	}

	// ---- Open redirect: 3xx com Location externo ao escopo ----
	if r.Status >= 300 && r.Status < 400 {
		if loc := headerGet(r.RespHeaders, "Location"); loc != "" {
			redirect, perr := url.Parse(loc)
			if perr == nil {
				host := redirect.Host
				if host == "" {
					host = scopeHost // relativo: mantem escopo
				}
				if host != scopeHost && !sameHost(host, scopeHost) {
					appendIf(Detection{
						Type: "redirect", Severity: store.SevLow,
						Evidence: map[string]any{"request_id": r.ID, "url": r.URL, "location": loc, "host": host, "note": "redirect para host fora do escopo do alvo"},
					})
				}
			}
		}
	}

	// ---- Secrets em JS: regex de credenciais em resp_body de JS/JSON ----
	if isScriptLike(r) {
		for _, sec := range secretPatterns {
			if m := sec.re.FindString(respBody); m != "" {
				appendIf(Detection{
					Type: "secret", Severity: store.SevHigh,
					Evidence: map[string]any{"request_id": r.ID, "url": r.URL, "kind": sec.kind, "found": truncateSecret(m), "note": "possivel segredo exposto (" + sec.kind + ")"},
				})
			}
		}
	}

	// ---- IDOR hint: request tem id numerico/UUID em path/param e mesma rota
	// com respostas de tamanhos divergentes (triagem grosseira, baixa certeza) ----
	if idorHintResp(r) {
		appendIf(Detection{
			Type: "IDOR", Severity: store.SevLow,
			Evidence: map[string]any{"request_id": r.ID, "url": r.URL, "note": "rota com id dinamico e resposta grande/divergente — sujeito a validacao de autorizacao (baixa certeza)"},
		})
	}

	return dets
}

func looksLikeMarkup(s string) bool {
	return strings.ContainsAny(s, "<>") || strings.ContainsAny(s, "\"'\n")
}

func matchAny(body string, pats []*regexp.Regexp) string {
	for _, p := range pats {
		if m := p.FindString(body); m != "" {
			return m
		}
	}
	return ""
}

func hasURLLikeParam(body, rawurl string) bool {
	// param com valor http(s):// em req_body ou query
	if m := urlParamRe.FindString(body); m != "" {
		return true
	}
	if u, err := url.Parse(rawurl); err == nil {
		for _, v := range u.Query() {
			for _, val := range v {
				if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
					return true
				}
			}
		}
	}
	return false
}

func containsPrivateIP(s string) bool {
	ips := ipRe.FindAllString(s, -1)
	for _, ip := range ips {
		if ip := net.ParseIP(ip); ip != nil && ip.IsPrivate() {
			return true
		}
	}
	return false
}

func containsMetadataIP(body string) bool {
	return metadataRe.MatchString(body)
}

func isScriptLike(r *store.Request) bool {
	ct := strings.ToLower(headerGet(r.RespHeaders, "Content-Type"))
	return strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "json") ||
		strings.HasSuffix(strings.ToLower(r.URL), ".js")
}

// headerGet extrai o primeiro valor de um header (default "").
func headerGet(h map[string][]string, name string) string {
	if vs, ok := h[name]; ok && len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func sameHost(a, b string) bool {
	return strings.TrimPrefix(a, "www.") == strings.TrimPrefix(b, "www.")
}

// idorHintResp: sinal fraco de IDOR — rota com id dinamico e resposta com
// tamanho acima da mediana subjetiva. Aqui apenas marcamos como candidato se
// houver segmento numerico/UUID na path.
func idorHintResp(r *store.Request) bool {
	if u, err := url.Parse(r.URL); err == nil {
		for _, seg := range strings.Split(u.Path, "/") {
			if idSegmentRe.MatchString(seg) && r.RespLen > 0 {
				return true
			}
		}
	}
	return false
}

func truncateSecret(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:6] + "..." + s[len(s)-4:]
}
