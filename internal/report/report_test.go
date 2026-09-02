package report

import (
	"strings"
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

func TestWriteMarkdown(t *testing.T) {
	findings := []*store.Finding{
		{Type: "XSS", Severity: store.SevHigh, URL: "https://x.example/a", Status: store.StatusOpen, Evidence: `{"payload":"x"}`},
		{Type: "IDOR", Severity: store.SevMed, URL: "https://x.example/u/1", Status: store.StatusConfirmed},
	}
	b, err := WriteMarkdown("x.example", findings)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "XSS") || !strings.Contains(s, "x.example") || !strings.Contains(s, "2 findings") {
		t.Fatalf("markdown incompleto: %s", s)
	}
}

func TestWriteHTML(t *testing.T) {
	findings := []*store.Finding{
		{Type: "SQLi", Severity: store.SevCrit, URL: "https://x.example/q", Status: store.StatusOpen},
	}
	b, err := WriteHTML("x.example", findings)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "<html") || !strings.Contains(s, "SQLi") || !strings.Contains(s, "1 high/critical") {
		t.Fatalf("html incompleto: %s", s)
	}
}
