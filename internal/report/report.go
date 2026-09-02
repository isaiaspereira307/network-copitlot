// Package report implements v5.1 export de findings em Markdown e HTML com
// template HackerOne-style (titulo, descricao, steps, impacto, fix).
package report

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// rowView e a visao serializavel de um finding p/ templates.
type rowView struct {
	Type     string
	Severity string
	URL      string
	Status   string
	Notes    string
	Evidence string
	TS       string
	High     bool
}

// view converte findings p/ visualizacoes de template e conta high/critical.
func view(findings []*store.Finding) ([]rowView, int) {
	vv := make([]rowView, 0, len(findings))
	high := 0
	for _, f := range findings {
		isHigh := f.Severity == store.SevCrit || f.Severity == store.SevHigh
		if isHigh {
			high++
		}
		ts := ""
		if f.Ts > 0 {
			ts = time.UnixMilli(f.Ts).Format("2006-01-02 15:04")
		}
		ev := ""
		if m := store.ParseEvidence(f.Evidence); m != nil {
			parts := make([]string, 0, len(m))
			for k, v := range m {
				parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			}
			ev = strings.Join(parts, " ")
		}
		vv = append(vv, rowView{
			Type: f.Type, Severity: string(f.Severity), URL: f.URL,
			Status: string(f.Status), Notes: f.Notes, Evidence: ev, TS: ts, High: isHigh,
		})
	}
	return vv, high
}

// WriteMarkdown serializa findings em Markdown HackerOne-ready.
func WriteMarkdown(target string, findings []*store.Finding) ([]byte, error) {
	rows, high := view(findings)
	data := map[string]any{
		"Target": target, "Rows": rows, "High": high, "Now": time.Now().Format("2006-01-02 15:04"),
	}
	t, err := template.New("md").Parse(mdTpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteHTML serializa findings em HTML (renderizavel p/ PDF via headless).
func WriteHTML(target string, findings []*store.Finding) ([]byte, error) {
	rows, high := view(findings)
	data := map[string]any{
		"Target": target, "Rows": rows, "High": high, "Now": time.Now().Format("2006-01-02 15:04"),
	}
	t, err := template.New("html").Parse(htmlTpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const mdTpl = `# Vulnerability Report — {{.Target}}
Generated: {{.Now}} — {{len .Rows}} findings, {{.High}} high/critical

{{range $i, $r := .Rows}}## {{$i}}: {{$r.Type}} @ {{$r.URL}}
- **Severity:** {{$r.Severity}}
- **Status:** {{$r.Status}}
- **Timestamp:** {{$r.TS}}
{{if $r.Notes}}- **Notes:** {{$r.Notes}}
{{end}}- **Evidence:** {{$r.Evidence}}

**Steps to reproduce:**
1.
2.

**Impact:** ...
**Proposed fix:** ...

---
{{end}}`

const htmlTpl = `<!doctype html><html lang="pt"><head><meta charset="utf-8">
<title>Report — {{.Target}}</title>
<style>body{font-family:system-ui,sans-serif;margin:2rem;color:#111}
table{border-collapse:collapse;width:100%;margin-top:1rem}
th,td{border:1px solid #ccc;padding:.5rem;text-align:left;vertical-align:top}
th{background:#eee}.high{background:#fdecea;color:#b00020;font-weight:700}
h2{font-size:1.1rem;margin-top:1.5rem}</style>
</head><body>
<h1>Vulnerability Report — {{.Target}}</h1>
<p>Generated {{.Now}} — {{len .Rows}} findings, {{.High}} high/critical</p>
<table><thead><tr><th>#</th><th>Type</th><th>Severity</th><th>URL</th><th>Status</th><th>Evidence</th></tr></thead>
<tbody>{{range $i, $r := .Rows}}<tr>
<td>{{$i}}</td><td>{{$r.Type}}</td>
<td class="{{if $r.High}}high{{end}}">{{$r.Severity}}</td>
<td><a href="{{$r.URL}}">{{$r.URL}}</a></td><td>{{$r.Status}}</td><td>{{$r.Evidence}}</td>
</tr>{{end}}</tbody></table>
</body></html>`
