package scanner

import (
	"strings"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// Active payloads SAO enviados ao host (diferente do passivo). Todos sao
// considerados "seguros"/nao-destrutivos e sao rate-limited. A lista de
// payloads destrutivos (DROP TABLE, rm -rf, etc) e explicitamente banida.
type activePayload struct {
	Type    string // XSS | SQLi | SSRF | redirect
	Payload string
}

var activePayloads = []activePayload{
	{Type: "XSS", Payload: `<script>alert(1)</script>`},
	{Type: "XSS", Payload: `"><svg onload=alert(1)>`},
	{Type: "XSS", Payload: `'><img src=x onerror=alert(1)>`},
	{Type: "SQLi", Payload: `'`},
	{Type: "SQLi", Payload: `' OR 1=1-- -`},
	{Type: "SQLi", Payload: `1 UNION SELECT 1-- -`},
	{Type: "SSRF", Payload: `http://127.0.0.1`},
	{Type: "SSRF", Payload: `http://169.254.169.254/latest/meta-data/`},
	{Type: "redirect", Payload: `https://evil.example`},
	{Type: "redirect", Payload: `//evil.example`},
}

// destructivePayloads sao proibidos (PRD v4.1 ban list).
var destructivePayloads = []string{
	"DROP TABLE", "DROP DATABASE", "rm -rf", "; rm -rf", "--fetch-ssl", "shutdown", "reboot",
}

// ActiveRequest e um request a enviar: baseado num capturado, com um payload
// injetado num ponto. Executado sob scope guard e com rate limit agressivo.
type ActiveRequest struct {
	BaseID   int64
	Tech     string // XSS | SQLi | SSRF | redirect
	Point    string // url | body | query:<p> | header:<n>
	Payload  string
	Redacted string
}

// BuildActiveRequests monta a lista de requests ativos a partir dos requests
// capturados (So injeta se o payload nao for banido). Limitado para nao
// explodir: maxActiveTotal (cap de seguranca).
func BuildActiveRequests(reqs []*store.Request, maxTotal int) []ActiveRequest {
	var out []ActiveRequest
	for _, r := range reqs {
		for _, ap := range activePayloads {
			if isDestructive(ap.Payload) {
				continue
			}
			ar := ActiveRequest{BaseID: r.ID, Tech: ap.Type, Point: "query:q", Payload: ap.Payload, Redacted: redactPayload(ap.Payload)}
			// so envia se o request tem query param ou corpo (alvo fuzzavel)
			if hasQuery(r.URL) || len(r.ReqBody) > 0 {
				out = append(out, ar)
				if maxTotal > 0 && len(out) >= maxTotal {
					return out
				}
			}
		}
	}
	return out
}

func isDestructive(p string) bool {
	up := strings.ToUpper(p)
	for _, d := range destructivePayloads {
		if strings.Contains(up, strings.ToUpper(d)) {
			return true
		}
	}
	return false
}

func hasQuery(rawurl string) bool {
	return strings.Contains(rawurl, "?")
}

// redactPayload limita o payload a 60 chars nos logs (frugal + nao despejar).
func redactPayload(p string) string {
	if len(p) <= 60 {
		return p
	}
	return p[:60] + "..."
}

// ActiveJobResult e o resultado de um request ativo enviado.
type ActiveJobResult struct {
	BaseID   int64  `json:"base_id"`
	Tech     string `json:"tech"`
	Point    string `json:"point"`
	Payload  string `json:"payload"` // redacted
	Status   int    `json:"status"`
	RespLen  int    `json:"resp_len"`
	Reflected bool  `json:"reflected"`
	ReplayID int64  `json:"replay_id"`
	Err      string `json:"err,omitempty"`
	TimeMs   int64  `json:"time_ms"`
}
