package mcpserver

import (
	"testing"
)

func TestNew_NotNil(t *testing.T) {
	s := New(nil, nil, nil)
	if s == nil {
		t.Fatal("New returned nil")
	}
}
