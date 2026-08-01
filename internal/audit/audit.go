package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

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
