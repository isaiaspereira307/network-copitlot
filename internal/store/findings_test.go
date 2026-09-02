package store

import (
	"path/filepath"
	"testing"
)

func TestFindingsCRUD(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "requests.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	id, err := st.AddFinding(&Finding{
		Type:      "SQLi",
		Severity:  SevHigh,
		URL:       "https://x/login",
		RequestID: 1,
		Evidence:  `{"snippet":"sql syntax"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("id=%d", id)
	}

	f, err := st.GetFinding(id)
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != StatusOpen || f.Type != "SQLi" {
		t.Fatalf("unexpected: %+v", f)
	}

	if err := st.SetFindingStatus(id, StatusConfirmed); err != nil {
		t.Fatal(err)
	}
	f, _ = st.GetFinding(id)
	if f.Status != StatusConfirmed {
		t.Fatalf("status=%s", f.Status)
	}

	if err := st.AddFindingNote(id, "validado manualmente"); err != nil {
		t.Fatal(err)
	}
	f, _ = st.GetFinding(id)
	if f.Notes != "validado manualmente" {
		t.Fatalf("notes=%q", f.Notes)
	}

	list, err := st.ListFindings("", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len=%d", len(list))
	}
}
