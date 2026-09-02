package crawler

import (
	"testing"
)

func TestCrawl_discoversSameHostAndRespectsRobots(t *testing.T) {
	seeds := []string{"https://example.com/"}
	hits := map[string]string{}
	fetcher := func(rawurl string) (int, []byte, error) {
		hits[rawurl] = rawurl
		switch rawurl {
		case "https://example.com/":
			return 200, []byte(`<a href="/about">About</a><a href="/admin/secret">A</a><a href="https://other.com/x">X</a>`), nil
		case "https://example.com/robots.txt":
			return 200, []byte("User-agent: *\nDisallow: /admin/\n"), nil
		case "https://example.com/about":
			return 200, []byte(`<a href="/contact">Contact</a>`), nil
		case "https://example.com/contact":
			return 200, []byte("contact"), nil
		default:
			return 200, []byte("default"), nil
		}
	}
	res, err := Crawl(seeds, 2, 0, fetcher)
	if err != nil {
		t.Fatalf("crawl: %v", err)
	}
	fetched := map[string]bool{}
	for _, u := range res.Fetched {
		fetched[u] = true
	}
	// /admin/secret bloqueado por robots
	if fetched["https://example.com/admin/secret"] {
		t.Errorf("robots disallow falhou: visitou /admin/secret")
	}
	// same-host /about visitado
	if !fetched["https://example.com/about"] {
		t.Errorf("nao visitou /about: %v", fetched)
	}
	// pagina de outro host nao deve entrar
	if res.Statuses["https://other.com/x"] > 0 {
		t.Errorf("crawl saiu do host primario")
	}
}
