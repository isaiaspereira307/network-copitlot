package scanner

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// --- regexes usados pelos detectores ---------------------------------------

var (
	sqliPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)sql syntax`),
		regexp.MustCompile(`(?i)mysql_[a-z_]+`),
		regexp.MustCompile(`(?i)ORA-\d{5}`),
		regexp.MustCompile(`(?i)pg_?query|postgresql.*error`),
		regexp.MustCompile(`(?i)sqlite.*syntax`),
		regexp.MustCompile(`(?i)unclosed quotation mark`),
		regexp.MustCompile(`(?i)SQLSTATE\[`),
	}
	urlParamRe  = regexp.MustCompile(`(?i)(url|uri|target|redirect|next|callback|return|dest|destino)=\s*["']?https?://`)
	ipRe        = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	metadataRe   = regexp.MustCompile(`(?i)169\.254\.169\.254|instance-metadata|cloud-metadata|user-data`)
	idSegmentRe = regexp.MustCompile(`(?i)^\d{2,}$|^[0-9a-f]{8}-[0-9a-f-]{36}$|^[0-9a-f]{24,}$`)
)

type secretRule struct {
	kind string
	re   *regexp.Regexp
}

var secretPatterns = []secretRule{
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}\b`)},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
	{"Stripe key", regexp.MustCompile(`\bsk_live_[0-9A-Za-z]{24,}\b`)},
	{"Generic private key", regexp.MustCompile(`(?m)-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`)},
}

// --- jobs de scan passivo --------------------------------------------------

// Job roda o scanner passivo sobre todos os requests capturados do alvo; nao
// executa nenhum request. Resulta numa lista de deteccoes por request, que o
// harness persiste como findings no store.
type Job struct {
	Total   int
	Hits    map[string]int // type -> count
	ByType  map[string][]Detection
}

// RunPassive percorre os requests e aplica os detectores; scopeHost e o host
// do alvo ativo (para avaliar redirects externos).
func RunPassive(reqs []*store.Request, scopeHost string) *Job {
	j := &Job{Hits: map[string]int{}, ByType: map[string][]Detection{}}
	for _, r := range reqs {
		j.Total++
		for _, d := range runDetectors(r, scopeHost) {
			j.Hits[d.Type]++
			j.ByType[d.Type] = append(j.ByType[d.Type], d)
		}
	}
	return j
}

// --- passive sitemap -------------------------------------------------------

// SitemapNode representa um endpoint deduplicado da arvore de requisicoes.
type SitemapNode struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Hits   int    `json:"hits"`
}

// BuildSitemap deriva a arvore de endpoints do trafego capturado (deduplicado
// por method+path, segmentos dinamicos colapsados em {id}), similar a
// list_endpoints mas orientado a arvore de navegacao.
func BuildSitemap(reqs []*store.Request) []SitemapNode {
	type key struct {
		method, path string
	}
	counts := map[key]int{}
	for _, r := range reqs {
		u, err := url.Parse(r.URL)
		if err != nil {
			continue
		}
		path := normalizePath(u.Path)
		k := key{method: r.Method, path: path}
		counts[k]++
	}
	out := make([]SitemapNode, 0, len(counts))
	for k, c := range counts {
		out = append(out, SitemapNode{Method: k.method, Path: k.path, Hits: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

var pathSegRe = regexp.MustCompile(`(?i)^\d+$|^[0-9a-f]{8}-[0-9a-f-]{36}$|^[0-9a-f]{24,}$`)

func normalizePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if pathSegRe.MatchString(s) {
			segs[i] = "{id}"
		}
	}
	return strings.Join(segs, "/")
}
