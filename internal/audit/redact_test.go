package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Redige valores de chaves que parecem carregar segredo.
// Inspirado em pentest-copilot/backend/src/services/mcp-tools.service.ts
// (redactMcpArgs) — mas aplicado na escrita do audit.log.
func TestLogger_RedactsSensitiveFields(t *testing.T) {
	dir := t.TempDir()
	l, err := New(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	in := map[string]any{
		"host":     "api.empresa.com",
		"password": "hunter2",
		"token":    "abc.def.ghi",
		"api_key":  "sk-12345",
		"username": "alice", // NAO deve ser redigido
		"nested": map[string]any{
			"privateKey": "-----BEGIN RSA PRIVATE KEY-----",
			"public":     "ok",
		},
		"list": []any{
			map[string]any{"secret": "shh", "ok": true},
		},
	}
	if err := l.Log(Event{Ts: time.Unix(0, 0), Tool: "test", Action: "x", Detail: in}); err != nil {
		t.Fatal(err)
	}
	l.Close()

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(data))
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("invalid json: %v\nraw: %s", err, line)
	}
	detail := got["detail"].(map[string]any)

	// Campos sensiveis redigidos.
	for _, k := range []string{"password", "token", "api_key"} {
		v, ok := detail[k]
		if !ok {
			t.Errorf("detail.%s missing", k)
			continue
		}
		if v != "[redacted]" {
			t.Errorf("detail.%s = %v, want [redacted]", k, v)
		}
	}
	// Campos nao-sensiveis intactos.
	if detail["host"] != "api.empresa.com" {
		t.Errorf("host redacted wrongly: %v", detail["host"])
	}
	if detail["username"] != "alice" {
		t.Errorf("username redacted wrongly: %v", detail["username"])
	}
	// Aninhado.
	nested := detail["nested"].(map[string]any)
	if nested["privateKey"] != "[redacted]" {
		t.Errorf("nested.privateKey = %v", nested["privateKey"])
	}
	if nested["public"] != "ok" {
		t.Errorf("nested.public redacted wrongly: %v", nested["public"])
	}
	// Em slice de maps.
	first := detail["list"].([]any)[0].(map[string]any)
	if first["secret"] != "[redacted]" {
		t.Errorf("list[0].secret = %v", first["secret"])
	}
	if first["ok"] != true {
		t.Errorf("list[0].ok redacted wrongly: %v", first["ok"])
	}
}

func TestRedact_PreservesNonStringSensitiveValues(t *testing.T) {
	// Valores nao-string (ex: bool, numero) sao preservados mesmo em chave
	// sensivel: redacao so se aplica a strings (credenciais sao texto).
	got := redact(map[string]any{
		"token":   42,
		"isAdmin": true,
	})
	m := got.(map[string]any)
	if m["token"] != 42 {
		t.Errorf("non-string token should stay: %v", m["token"])
	}
	if m["isAdmin"] != true {
		t.Errorf("isAdmin should stay: %v", m["isAdmin"])
	}
}

func TestRedact_HandlesNil(t *testing.T) {
	if got := redact(nil); got != nil {
		t.Errorf("nil -> nil, got %v", got)
	}
}
