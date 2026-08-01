package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogger_WritesJSONLines(t *testing.T) {
	dir := t.TempDir()
	l, err := New(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if err := l.Log(Event{Ts: time.Unix(0, 0), Tool: "create_project", Action: "create", Detail: map[string]any{"name": "P1"}}); err != nil {
		t.Fatal(err)
	}
	if err := l.Log(Event{Ts: time.Unix(1, 0), Tool: "add_target", Action: "add", Detail: map[string]any{"host": "x.com"}}); err != nil {
		t.Fatal(err)
	}
	l.Close()

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("line 0 invalid JSON: %v", err)
	}
	if e.Tool != "create_project" {
		t.Errorf("tool = %q", e.Tool)
	}
}

func TestLogger_ConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	l, err := New(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			l.Log(Event{Tool: "x", Action: "a", Detail: i})
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	l.Close()
	data, _ := os.ReadFile(filepath.Join(dir, "audit.log"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
}
