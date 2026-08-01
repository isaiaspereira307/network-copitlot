package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenSQLite(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteStore_InsertAndGet(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Insert(&Request{
		Ts:          1700000000000,
		Method:      "GET",
		URL:         "https://api.empresa.com/users",
		ReqHeaders:  map[string][]string{"User-Agent": {"test"}},
		Status:      200,
		RespHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RespLen:     42,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id == 0 {
		t.Fatal("id must be > 0")
	}
	got, err := s.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Method != "GET" || got.URL != "https://api.empresa.com/users" || got.Status != 200 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

func TestSQLiteStore_List_Limit(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 5; i++ {
		if _, err := s.Insert(&Request{Ts: int64(i), Method: "GET", URL: "https://x.com/", ReqHeaders: map[string][]string{}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(ListFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
}

func TestSQLiteStore_Count(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 7; i++ {
		if _, err := s.Insert(&Request{Ts: int64(i), Method: "GET", URL: "https://x.com/", ReqHeaders: map[string][]string{}}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Errorf("count = %d, want 7", n)
	}
}
