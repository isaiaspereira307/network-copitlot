package proxy

import (
	"path/filepath"
	"testing"

	"github.com/isaias/network-copitlot/internal/store"
)

func TestNew_NotNil(t *testing.T) {
	dir := t.TempDir()
	s, err := store.OpenSQLite(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := New(s)
	if p == nil {
		t.Fatal("New returned nil")
	}
}
