package har

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

func TestWriteHAR(t *testing.T) {
	reqs := []*store.Request{
		{Method: "GET", URL: "https://a.com/x", Status: 200, RespLen: 120,
			ReqHeaders:  map[string][]string{"Accept": {"text/html"}},
			RespHeaders: map[string][]string{"Content-Type": {"text/html"}}},
		{Method: "POST", URL: "https://a.com/y", Status: 500, RespLen: 30,
			ReqBody:     []byte(`{"k":1}`),
			ReqHeaders:  map[string][]string{"Content-Type": {"application/json"}},
			RespHeaders: map[string][]string{"Content-Type": {"application/json"}}},
	}
	raw, err := WriteHAR("a.com", reqs)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Log struct {
			Version string `json:"version"`
			Creator struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"creator"`
			Entries []struct {
				Request struct {
					Method  string `json:"method"`
					URL     string `json:"url"`
					Headers []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"headers"`
					BodySize int `json:"bodySize"`
				} `json:"request"`
				Response struct {
					Status   int `json:"status"`
					BodySize int `json:"bodySize"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("HAR invalido: %v", err)
	}
	if doc.Log.Version != "1.2" {
		t.Errorf("version = %q, want 1.2", doc.Log.Version)
	}
	if len(doc.Log.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(doc.Log.Entries))
	}
	e := doc.Log.Entries[1]
	if e.Request.Method != "POST" || e.Request.URL != "https://a.com/y" {
		t.Errorf("entry[1] = %s %s", e.Request.Method, e.Request.URL)
	}
	if e.Response.Status != 500 {
		t.Errorf("status = %d, want 500", e.Response.Status)
	}
	if e.Request.BodySize != len(`{"k":1}`) {
		t.Errorf("bodySize = %d, want %d", e.Request.BodySize, len(`{"k":1}`))
	}
	if len(doc.Log.Entries[0].Request.Headers) != 1 {
		t.Errorf("entry[0] request headers = %d, want 1", len(doc.Log.Entries[0].Request.Headers))
	}
	// metadata only: nenhum body serializado no HAR
	if strings.Contains(string(raw), `"postData"`) {
		t.Error("HAR nao deve conter conteudo de body (postData)")
	}
	if strings.Contains(string(raw), `"k"`) || strings.Contains(string(raw), `{"k":1}`) {
		t.Error("corpo do request vazou no HAR")
	}
}
