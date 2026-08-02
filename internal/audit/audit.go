package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// sensitiveKeyRe casa chaves que provavelmente carregam segredo.
// Case-insensitive. Casa: password, passwd, passphrase, privatekey,
// apikey, api_key, secret, token, credential, authorization.
var sensitiveKeyRe = regexp.MustCompile(`(?i)(password|passphrase|privatekey|api_?key|secret|token|credential|authorization)`)

const redactedSentinel = "[redacted]"

// redact percorre o valor e substitui strings em chaves sensiveis por
// redactedSentinel. Nao-string (bool, numero) e preservado: redacao so
// se aplica a texto (credenciais). Mapas e slices sao percorridos
// recursivamente; valores primitivos fora de chave sensivel ficam intactos.
//
// Inspirado em pentest-copilot/backend/src/services/mcp-tools.service.ts
// (funcao redactMcpArgs). Reescrito do zero em Go.
func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if _, isStr := val.(string); isStr && sensitiveKeyRe.MatchString(k) {
				if val == "" {
					out[k] = val
				} else {
					out[k] = redactedSentinel
				}
				continue
			}
			out[k] = redact(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = redact(val)
		}
		return out
	default:
		return v
	}
}

const (
	dirName  = ".mcp-proxy"
	fileName = "audit.log"
)

type Event struct {
	Ts     time.Time `json:"ts"`
	Tool   string    `json:"tool"`
	User   string    `json:"user,omitempty"`
	Action string    `json:"action"`
	Detail any       `json:"detail,omitempty"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName, fileName), nil
}

type Logger struct {
	mu   sync.Mutex
	file *os.File
}

func New(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f}, nil
}

func (l *Logger) Log(e Event) error {
	if e.Ts.IsZero() {
		e.Ts = time.Now().UTC()
	}
	if e.Detail != nil {
		e.Detail = redact(e.Detail)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(l.file, "%s\n", data)
	return err
}

func (l *Logger) Close() error {
	return l.file.Close()
}
