// Package summarize produz resumos compactos de corpos HTTP por content-type:
// HTML (forms/links/scripts/comments interessantes), JSON (chaves+tipos, nunca
// valores) e JS (endpoints/calls/tokens). Pilares: frugalidade de tokens e
// outputs estruturados estaveis — nunca despejar o body inteiro.
package summarize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// MaxBodyBytes e o teto de corpo analisado: acima dele o summary vem de um
// prefixo limitado e Truncated=true sinaliza ao chamador.
const MaxBodyBytes = 64 * 1024

const (
	maxJSONDepth  = 3  // profundidade da arvore de chaves; abaixo colapsa "object"/"array"
	maxLinks      = 50 // caps por categoria: frugalidade de tokens
	maxExtScripts = 20
	maxComments   = 10
	maxCommentLen = 200
	maxJSUrls     = 30
	maxJSCalls    = 20
	maxJSTokens   = 10
)

// Result e o resumo compacto: Kind identifica o sumarizador usado; Detail
// carrega as chaves especificas; Truncated/TotalLen descrevem o corpo original.
type Result struct {
	Kind      string
	Truncated bool
	TotalLen  int
	Detail    map[string]any
	Note      string
}

// FormInfo e um <form> extraido do HTML.
type FormInfo struct {
	Action string   `json:"action"`
	Method string   `json:"method"`
	Fields []string `json:"fields"`
}

// CallInfo e uma chamada fetch/XHR/axios extraida do JS.
type CallInfo struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

// TokenInfo e um segredo provavel (JWT/AWS/API key) com hint truncado.
type TokenInfo struct {
	Type string `json:"type"`
	Hint string `json:"hint"`
}

// Body detecta o tipo por content-type (com fallback em
// http.DetectContentType + sniff direto de JSON) e despacha para o
// sumarizador adequado. Corpos maiores que max sao cortados no prefixo.
func Body(ct string, body []byte, max int) Result {
	total := len(body)
	truncated := total > max
	if truncated {
		body = body[:max]
	}
	res := Result{Truncated: truncated, TotalLen: total}
	switch kindOf(ct, body) {
	case "html":
		res.Kind = "html"
		res.Detail = summarizeHTML(body)
	case "json":
		res.Kind = "json"
		res.Detail, res.Note = summarizeJSON(body)
		if res.Note != "" && truncated {
			res.Note += " (corpo possivelmente truncado no prefixo)"
		}
	case "js":
		res.Kind = "js"
		res.Detail = summarizeJS(body)
	case "text":
		res.Kind = "text"
		res.Detail = summarizeText(body)
	default:
		res.Kind = "other"
		res.Note = fmt.Sprintf("sem sumarizador para content-type %q", ct)
	}
	return res
}

// kindOf resolve o sumarizador: header primeiro; fallback sniff
// (http.DetectContentType) quando o header vem vazio; depois sniff direto de
// JSON por prefixo '{'/'[' (cobre content-type "text/plain" ambiguo).
func kindOf(ct string, body []byte) string {
	ct = strings.ToLower(ct)
	switch {
	case strings.Contains(ct, "html"):
		return "html"
	case strings.Contains(ct, "json"):
		return "json"
	case strings.Contains(ct, "javascript") || strings.Contains(ct, "ecmascript"):
		return "js"
	}
	if ct == "" {
		if d := http.DetectContentType(body); strings.Contains(d, "json") {
			return "json"
		}
	}
	t := bytes.TrimSpace(body)
	if len(t) > 0 && (t[0] == '{' || t[0] == '[') {
		return "json"
	}
	if strings.Contains(ct, "text") || ct == "" {
		return "text"
	}
	return "other"
}

// collectAttrs le todos os atributos do token corrente do tokenizer.
func collectAttrs(z *html.Tokenizer, hasAttr bool) map[string]string {
	attrs := map[string]string{}
	for hasAttr {
		k, v, more := z.TagAttr()
		attrs[string(k)] = string(v)
		hasAttr = more
	}
	return attrs
}

var (
	inlineKeywords  = regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16}|eyJ[A-Za-z0-9_\-]{10,}|api[_-]?key|secret|password|token|credential|authorization|bearer)`)
	commentInterest = regexp.MustCompile(`(?i)(todo|fixme|hack|secret|password|api[_-]?key|token|creds?|credential|vuln|insecure|weak|debug|remove|deprecated)`)
)

// summarizeHTML varre o body com o tokenizer de golang.org/x/net/html e
// extrai: forms (action/method/input names), links href (dedup), scripts
// externos (src) + inline (contagem + keywords interessantes) e comments com
// conteudo suspeito. Sempre limitado por caps — nunca o HTML inteiro.
func summarizeHTML(body []byte) map[string]any {
	z := html.NewTokenizer(bytes.NewReader(body))
	var (
		forms     []FormInfo
		formIdx   = -1
		formDepth = 0
		links     []string
		seenLink  = map[string]bool{}
		exts      []string
		seenExt   = map[string]bool{}
		inline    = 0
		inlineKW  = map[string]bool{}
		pending   = false // texto do proximo script inline
		comments  []string
	)
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			attrs := collectAttrs(z, hasAttr)
			switch string(name) {
			case "form":
				if tt == html.StartTagToken {
					method := attrs["method"]
					if method == "" {
						method = "GET"
					}
					forms = append(forms, FormInfo{Action: attrs["action"], Method: method})
					formIdx = len(forms) - 1
					formDepth++
				}
			case "input", "select", "textarea":
				if formDepth > 0 && formIdx >= 0 {
					if n := attrs["name"]; n != "" {
						f := &forms[formIdx]
						if !slices.Contains(f.Fields, n) {
							f.Fields = append(f.Fields, n)
						}
					}
				}
			case "a", "link":
				if h := attrs["href"]; h != "" && len(links) < maxLinks && !seenLink[h] {
					seenLink[h] = true
					links = append(links, h)
				}
			case "script":
				if src := attrs["src"]; src != "" {
					if len(exts) < maxExtScripts && !seenExt[src] {
						seenExt[src] = true
						exts = append(exts, src)
					}
				} else if tt == html.StartTagToken {
					inline++
					pending = true
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if string(name) == "form" && formDepth > 0 {
				formDepth--
				if formDepth == 0 {
					formIdx = -1
				}
			}
		case html.TextToken:
			if pending {
				pending = false
				for _, kw := range inlineKeywords.FindAllString(string(z.Text()), -1) {
					inlineKW[kw] = true
				}
			}
		case html.CommentToken:
			c := strings.TrimSpace(string(z.Text()))
			if c != "" && commentInterest.MatchString(c) && len(comments) < maxComments {
				if len(c) > maxCommentLen {
					c = c[:maxCommentLen]
				}
				comments = append(comments, c)
			}
		}
	}
	d := map[string]any{
		"forms":           forms,
		"links":           links,
		"scripts_external": exts,
		"scripts_inline":  inline,
	}
	if len(comments) > 0 {
		d["comments"] = comments
	}
	if len(inlineKW) > 0 {
		kw := make([]string, 0, len(inlineKW))
		for k := range inlineKW {
			kw = append(kw, k)
		}
		slices.Sort(kw)
		d["inline_keywords"] = kw
	}
	return d
}

// summarizeJSON decodifica o body e devolve a arvore de chaves+tipos ate
// maxJSONDepth. Valores nunca sao incluidos; arrays viram [tipo-do-primeiro].
// Objetos/arrays no limite de profundidade colapsam em "object"/"array".
func summarizeJSON(body []byte) (map[string]any, string) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return map[string]any{"error": "invalid JSON"}, fmt.Sprintf("JSON invalido: %v", err)
	}
	return map[string]any{"keys": walkJSON(v, 0)}, ""
}

func walkJSON(v any, depth int) any {
	switch x := v.(type) {
	case map[string]any:
		if depth >= maxJSONDepth {
			return "object"
		}
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[k] = walkJSON(val, depth+1)
		}
		return m
	case []any:
		if depth >= maxJSONDepth || len(x) == 0 {
			return "array"
		}
		return []any{walkJSON(x[0], depth+1)}
	case json.Number:
		return "number"
	case string:
		return "string"
	case bool:
		return "boolean"
	case nil:
		return "null"
	}
	return "?"
}

var (
	jsURLRe   = regexp.MustCompile(`https?://[^\s'"<>()\\]+`)
	jsFetchRe = regexp.MustCompile(`fetch\s*\(\s*["']([^"']+)["']`)
	jsOpenRe  = regexp.MustCompile(`\.open\s*\(\s*["'](GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)["']\s*,\s*["']([^"']+)["']`)
	jsAxiosRe = regexp.MustCompile(`axios\.(?:get|post|put|delete|patch|head)\s*\(\s*["']([^"']+)["']`)
	jwtRe     = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`)
	awsRe     = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	keyRe     = regexp.MustCompile(`(?i)(api[_-]?key|secret|password|access[_-]?token|auth[_-]?token|bearer)\s*[:=]\s*["'][^"']{8,}["']`)
)

// summarizeJS varre o body com regex por: URLs de endpoint, padroes de chamada
// fetch/XHR/axios (com o primeiro argumento literal como alvo) e tokens
// provaveis (JWT, AWS, API keys) — hints truncados a 8 chars, sem despejar o
// segredo. Sempre limitado por caps.
func summarizeJS(body []byte) map[string]any {
	s := string(body)
	d := map[string]any{}

	var urls []string
	seenURL := map[string]bool{}
	for _, m := range jsURLRe.FindAllString(s, -1) {
		u := strings.TrimRight(m, ".,;:])}\"'")
		if !seenURL[u] {
			seenURL[u] = true
			urls = append(urls, u)
		}
		if len(urls) >= maxJSUrls {
			break
		}
	}
	if len(urls) > 0 {
		d["urls"] = urls
	}

	var calls []CallInfo
	addCall := func(kind, target string) {
		target = strings.TrimRight(target, ".,;:])}\"'")
		if target == "" {
			return
		}
		if slices.ContainsFunc(calls, func(c CallInfo) bool { return c == CallInfo{Kind: kind, Target: target} }) {
			return
		}
		if len(calls) < maxJSCalls {
			calls = append(calls, CallInfo{Kind: kind, Target: target})
		}
	}
	for _, m := range jsFetchRe.FindAllStringSubmatch(s, -1) {
		addCall("fetch", m[1])
	}
	for _, m := range jsOpenRe.FindAllStringSubmatch(s, -1) {
		addCall("xhr", m[2])
	}
	for _, m := range jsAxiosRe.FindAllStringSubmatch(s, -1) {
		addCall("axios", m[1])
	}
	if len(calls) > 0 {
		d["calls"] = calls
	}

	var tokens []TokenInfo
	addToken := func(typ, val string) {
		if len(tokens) >= maxJSTokens {
			return
		}
		h := val
		if len(h) > 8 {
			h = h[:8]
		}
		tokens = append(tokens, TokenInfo{Type: typ, Hint: h + "..."})
	}
	for _, m := range jwtRe.FindAllString(s, -1) {
		addToken("jwt", m)
	}
	for _, m := range awsRe.FindAllString(s, -1) {
		addToken("aws_key", m)
	}
	for _, m := range keyRe.FindAllStringSubmatch(s, -1) {
		addToken("api_key", m[2])
	}
	if len(tokens) > 0 {
		d["tokens"] = tokens
	}
	return d
}

// summarizeText e o fallback para text/*: preview curto (300 chars), nunca o
// body inteiro.
func summarizeText(body []byte) map[string]any {
	const max = 300
	s := strings.TrimSpace(string(body))
	trunc := false
	if len(s) > max {
		s = s[:max]
		trunc = true
	}
	return map[string]any{"preview": s, "preview_truncated": trunc}
}
