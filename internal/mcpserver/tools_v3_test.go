package mcpserver

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/isaias/network-copitlot/internal/store"
)

func TestListEndpointsTool_NoActiveTarget(t *testing.T) {
	s, _ := newTestServer(t)
	_, err := callTool(t, s, "list_endpoints", map[string]any{})
	if err == nil {
		t.Fatal("expected error with no active target")
	}
	if err.Error() != "nenhum alvo ativo: selecione um alvo com set_active_target" {
		t.Errorf("err = %q, want mensagem exata de alvo ativo", err.Error())
	}
}

func TestListEndpointsTool_ReturnsDedupedEndpoints(t *testing.T) {
	s, _ := newTestServer(t)
	_, _ = callTool(t, s, "create_project", map[string]any{"name": "P", "type": "bugbounty"})
	_, _ = callTool(t, s, "set_active_project", map[string]any{"name": "P"})
	_, _ = callTool(t, s, "add_target", map[string]any{"host": "api.empresa.com", "confirmed": true})
	_, _ = callTool(t, s, "set_active_target", map[string]any{"host": "api.empresa.com"})

	st, err := s.openStoreForActiveTarget()
	if err != nil || st == nil {
		t.Fatalf("open store: %v", err)
	}
	for i, url := range []string{
		"https://api.empresa.com/users/123",
		"https://api.empresa.com/users/456",
		"https://api.empresa.com/health",
	} {
		if _, err := st.Insert(&store.Request{Ts: time.Now().UnixMilli() + int64(i), Method: "GET", URL: url}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	out, err := callTool(t, s, "list_endpoints", map[string]any{})
	if err != nil {
		t.Fatalf("list_endpoints: %v", err)
	}
	var res struct {
		Count     int `json:"count"`
		Endpoints []struct {
			Method    string  `json:"method"`
			Path      string  `json:"path"`
			HitCount  int     `json:"hit_count"`
			SampleIDs []int64 `json:"sample_ids"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, out)
	}
	if res.Count != 2 || len(res.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got count=%d len=%d body=%s", res.Count, len(res.Endpoints), out)
	}
	byPath := map[string]struct {
		Hit  int
		IDs  []int64
	}{}
	for _, e := range res.Endpoints {
		if e.Method != "GET" {
			t.Errorf("method = %q, want GET", e.Method)
		}
		byPath[e.Path] = struct {
			Hit int
			IDs []int64
		}{e.HitCount, e.SampleIDs}
	}
	if ep := byPath["/users/{id}"]; ep.Hit != 2 || len(ep.IDs) != 2 {
		t.Errorf("/users/{id} = %+v, want hit 2, 2 sample ids", ep)
	}
	if ep := byPath["/health"]; ep.Hit != 1 || len(ep.IDs) != 1 {
		t.Errorf("/health = %+v, want hit 1, 1 sample id", ep)
	}
}
