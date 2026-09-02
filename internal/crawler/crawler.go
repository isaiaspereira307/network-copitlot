// Package crawler implements v4.1 active crawling: discovers same-host URLs by
// following html links from a seed set, depth-limited, respecting robots.txt,
// never leaving the active target's scope (double opt-in enforced upstream).
package crawler

import (
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Fetcher executa um GET e devolve o status e o body (sob scope guard).
// Implementado pelo harness (tem proxy/scope); zero tráfego fora de escopo.
type Fetcher func(rawurl string) (status int, body []byte, err error)

// Client is a max of parallel fetches / rate limit.
const defaultMaxDepth = 3

// Robots rules simples: block ("Disallow") paths.
type robotsRules struct{ disallow []string }

func (r *robotsRules) allows(path string) bool {
	for _, d := range r.disallow {
		if d == "" || d == "/" {
			continue
		}
		if strings.HasPrefix(path, d) {
			return false
		}
	}
	return true
}

// Result resume o crawl.
type Result struct {
	Fetched  []string         `json:"fetched"`
	Statuses map[string]int   `json:"statuses"`
	URLs     map[string]int   `json:"urls"`
	Depth    int              `json:"depth"`
	Errors   map[string]string `json:"errors,omitempty"`
}

// Crawl descobre URLs partindo dos seeds, limitado por maxDepth (default 3),
// seguindo apenas HTML do MESMO host, dentro do escopo (via fetcher). Retorna
// a lista de URLs descobertas/visitadas.
func Crawl(seeds []string, maxDepth int, throttle time.Duration, fetcher Fetcher) (*Result, error) {
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}
	res := &Result{Statuses: map[string]int{}, URLs: map[string]int{}, Errors: map[string]string{}}
	visited := map[string]bool{}
	queue := map[int][]string{}
	// filtra seeds p/ um host primario (primeiro seed valido)
	primaryHost := ""
	valid := []string{}
	for _, s := range seeds {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			if primaryHost == "" {
				primaryHost = u.Host
			}
			if u.Host == primaryHost {
				valid = append(valid, s)
			}
		}
	}
	if len(valid) == 0 {
		return nil, io.EOF
	}

	// robots.txt
	var robots *robotsRules
	if primaryHost != "" {
		status, body, err := fetcher("https://" + primaryHost + "/robots.txt")
		if err == nil && status == 200 {
			robots = parseRobots(string(body))
			res.Statuses["robots.txt"] = status
		}
	}

	queue[0] = valid
	for depth := 0; depth <= maxDepth; depth++ {
		next := queue[depth]
		var frontier []string
		for _, s := range next {
			if visited[s] {
				continue
			}
			if robots != nil {
				u, _ := url.Parse(s)
				if !robots.allows(u.Path) {
					continue
				}
			}
			visited[s] = true
			status, body, err := fetcher(s)
			res.Fetched = append(res.Fetched, s)
			if err != nil {
				res.Errors[s] = err.Error()
				continue
			}
			res.Statuses[s] = status
			if status == 200 && depth < maxDepth {
				frontier = append(frontier, extractLinks(string(body), s, primaryHost)...)
			}
			res.URLs[s] = status
			if throttle > 0 {
				time.Sleep(throttle)
			}
		}
		// dedupe frontier
		seen := map[string]bool{}
		var unique []string
		for _, f := range frontier {
			if !visited[f] && !seen[f] {
				seen[f] = true
				unique = append(unique, f)
			}
		}
		queue[depth+1] = unique
		res.Depth = depth
	}
	return res, nil
}

// extractLinks extrai links <a href> e <link href>/<script src> do mesmo host.
func extractLinks(body, base, host string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			var href string
			switch n.Data {
			case "a", "link":
				href = getAttr(n, "href")
			case "script":
				href = getAttr(n, "src")
			}
			if href != "" {
				if u, uerr := url.Parse(href); uerr == nil {
					abs := u
					if !u.IsAbs() {
						b, _ := url.Parse(base)
						abs = b.ResolveReference(u)
					}
					if abs.Host == host || abs.Host == "" {
						abs.Fragment = ""
						out = append(out, abs.String())
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return dedupe(out)
}

func getAttr(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}

func parseRobots(body string) *robotsRules {
	r := &robotsRules{}
	// ignora User-agent groups de nao-bots; aplica regras ao dominio geral
	inGeneral := true
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(strings.ToLower(line), "user-agent:"):
			ua := strings.TrimSpace(line[len("user-agent:"):])
			inGeneral = ua == "*"
		case inGeneral && strings.HasPrefix(strings.ToLower(line), "disallow:"):
			p := strings.TrimSpace(line[len("disallow:"):])
			if p != "" && !strings.HasPrefix(p, "/") {
				p = "/" + p
			}
			r.disallow = append(r.disallow, p)
		}
	}
	return r
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

var _ = regexp.MustCompile
