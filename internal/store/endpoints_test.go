package store

import (
	"testing"
)

// TestListEndpoints_NormalizesIDs (14.1): segmentos dinamicos de path
// (numerico, UUID, hex>=16, base64>=22) colapsam em {id}. Grupos por
// (method, normalized_path); hit_count = total de requests; sample_ids =
// ids mais recentes do grupo (max 5), em ordem id DESC.
func TestListEndpoints_NormalizesIDs(t *testing.T) {
	s := newTestStore(t)
	rows := []*Request{
		{Ts: 1, Method: "GET", URL: "https://api.empresa.com/users/123", ReqHeaders: map[string][]string{}},
		{Ts: 2, Method: "GET", URL: "https://api.empresa.com/users/456", ReqHeaders: map[string][]string{}},
		{Ts: 3, Method: "GET", URL: "https://api.empresa.com/health", ReqHeaders: map[string][]string{}},
		{Ts: 4, Method: "GET", URL: "https://api.empresa.com/users/9f8e7d6c-5b4a-4c3b-a2a1-0f1e2d3c4b5a", ReqHeaders: map[string][]string{}}, // UUID
		{Ts: 5, Method: "POST", URL: "https://api.empresa.com/items/9f8e7d6c5b4a3c2d1e0f9a8b7c6d5e4f", ReqHeaders: map[string][]string{}},   // hex 32
		{Ts: 6, Method: "POST", URL: "https://api.empresa.com/items/YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4", ReqHeaders: map[string][]string{}}, // base64 30
	}
	for i, r := range rows {
		if _, err := s.Insert(r); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	// +7 numericos em /users/{id}: prova hit_count nao-cap e cap de sample_ids.
	for i := 0; i < 7; i++ {
		if _, err := s.Insert(&Request{Ts: int64(7 + i), Method: "GET", URL: "https://api.empresa.com/users/9" + string(rune('0'+i)), ReqHeaders: map[string][]string{}}); err != nil {
			t.Fatalf("insert extra %d: %v", i, err)
		}
	}

	got, err := s.ListEndpoints()
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	byKey := map[string]*Endpoint{}
	for _, e := range got {
		byKey[e.Method+" "+e.Path] = e
	}

	e := byKey["GET /users/{id}"]
	if e == nil {
		t.Fatalf("missing GET /users/{id}, got keys: %v", byKey)
	}
	if e.HitCount != 10 {
		t.Errorf("GET /users/{id} hit_count = %d, want 10", e.HitCount)
	}
	if len(e.SampleIDs) != 5 {
		t.Errorf("GET /users/{id} sample_ids len = %d, want cap 5", len(e.SampleIDs))
	} else {
		want := []int64{13, 12, 11, 10, 9} // ids mais recentes, DESC
		for i := range want {
			if e.SampleIDs[i] != want[i] {
				t.Errorf("sample_ids = %v, want %v", e.SampleIDs, want)
				break
			}
		}
	}

	if e := byKey["GET /health"]; e == nil || e.HitCount != 1 || len(e.SampleIDs) != 1 || e.SampleIDs[0] != 3 {
		t.Errorf("GET /health = %+v, want hit 1 sample [3]", e)
	}

	if e := byKey["POST /items/{id}"]; e == nil || e.HitCount != 2 || len(e.SampleIDs) != 2 || e.SampleIDs[0] != 6 || e.SampleIDs[1] != 5 {
		t.Errorf("POST /items/{id} = %+v, want hit 2 sample [6 5]", e)
	}

	if n := len(got); n != 3 {
		t.Errorf("expected 3 endpoints, got %d: %+v", n, got)
	}
}
