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

func TestSQLiteStore_List_FiltersByMethodStatusHost(t *testing.T) {
	s := newTestStore(t)
	rows := []*Request{
		{Ts: 1, Method: "POST", URL: "https://api.empresa.com/users", Status: 200, ReqHeaders: map[string][]string{}},
		{Ts: 2, Method: "GET", URL: "https://api.empresa.com/users", Status: 200, ReqHeaders: map[string][]string{}},
		{Ts: 3, Method: "POST", URL: "https://api.empresa.com/login", Status: 401, ReqHeaders: map[string][]string{}},
		{Ts: 4, Method: "GET", URL: "https://outra.com/login", Status: 200, ReqHeaders: map[string][]string{}},
		{Ts: 5, Method: "DELETE", URL: "https://api.empresa.com/users/1", Status: 204, ReqHeaders: map[string][]string{}},
	}
	for _, r := range rows {
		if _, err := s.Insert(r); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	got, err := s.List(ListFilter{MethodFilter: "POST", StatusFilter: 200})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].Method != "POST" || got[0].Status != 200 || got[0].URL != "https://api.empresa.com/users" {
		t.Errorf("wrong row: %+v", got[0])
	}
}

func TestSQLiteStore_List_HostAndPathContains(t *testing.T) {
	s := newTestStore(t)
	rows := []*Request{
		{Ts: 1, Method: "GET", URL: "https://api.empresa.com/users", ReqHeaders: map[string][]string{}},
		{Ts: 2, Method: "GET", URL: "https://api.empresa.com/login", ReqHeaders: map[string][]string{}},
		{Ts: 3, Method: "GET", URL: "https://admin.outra.com/users", ReqHeaders: map[string][]string{}},
	}
	for _, r := range rows {
		if _, err := s.Insert(r); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	got, err := s.List(ListFilter{HostFilter: "empresa.com"})
	if err != nil {
		t.Fatalf("list host: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("host: got %d, want 2", len(got))
	}

	got, err = s.List(ListFilter{PathContains: "/users"})
	if err != nil {
		t.Fatalf("list path: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("path: got %d, want 2", len(got))
	}
}

func TestSQLiteStore_List_SinceIDOffset(t *testing.T) {
	s := newTestStore(t)
	var ids []int64
	for i := 0; i < 4; i++ {
		id, err := s.Insert(&Request{Ts: int64(i), Method: "GET", URL: "https://x.com/", ReqHeaders: map[string][]string{}})
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		ids = append(ids, id)
	}

	got, err := s.List(ListFilter{SinceID: ids[1]})
	if err != nil {
		t.Fatalf("list since: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("since: got %d, want 2", len(got))
	}

	got, err = s.List(ListFilter{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("list offset: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("offset: got %d, want 2", len(got))
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
	for _, sm := range got {
		if sm.Method != "GET" || sm.URL == "" {
			t.Errorf("summary fields missing: %+v", sm)
		}
	}
}

func TestSQLiteStore_GetDetail_TruncatesBody(t *testing.T) {
	s := newTestStore(t)
	big := make([]byte, 20480)
	for i := range big {
		big[i] = 'x'
	}
	id, err := s.Insert(&Request{
		Ts:          1,
		Method:      "POST",
		URL:         "https://api.empresa.com/upload",
		ReqHeaders:  map[string][]string{"Content-Type": {"application/octet-stream"}},
		ReqBody:     big,
		Status:      201,
		RespHeaders: map[string][]string{},
		RespBody:    big,
		RespLen:     len(big),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	d, err := s.GetDetail(id, "all", 8192, "")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !d.ReqBodyTruncated || !d.RespBodyTruncated {
		t.Errorf("expected both truncated, got req=%v resp=%v", d.ReqBodyTruncated, d.RespBodyTruncated)
	}
	if d.ReqTotalLen != 20480 || d.RespTotalLen != 20480 {
		t.Errorf("total len = %d/%d, want 20480/20480", d.ReqTotalLen, d.RespTotalLen)
	}
	if len(d.ReqBody) != 8192 || len(d.RespBody) != 8192 {
		t.Errorf("body len = %d/%d, want 8192/8192", len(d.ReqBody), len(d.RespBody))
	}
	if string(d.ReqBody[:16]) != "xxxxxxxxxxxxxxxx" {
		t.Errorf("body content corrupted: %q", d.ReqBody[:16])
	}

	wd, err := s.GetDetail(id, "all", 8192, "0-4096")
	if err != nil {
		t.Fatalf("detail range: %v", err)
	}
	if len(wd.ReqBody) != 4096 {
		t.Errorf("ranged body len = %d, want 4096", len(wd.ReqBody))
	}

	hd, err := s.GetDetail(id, "headers", 0, "")
	if err != nil {
		t.Fatalf("detail headers: %v", err)
	}
	if len(hd.ReqHeaders) == 0 || hd.ReqBody != nil {
		t.Errorf("headers-only include wrong: headers=%v body=%v", hd.ReqHeaders, hd.ReqBody)
	}

	if _, err := s.GetDetail(999999, "all", 8192, ""); err == nil {
		t.Error("expected error for missing id")
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
