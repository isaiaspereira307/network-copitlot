// Package har exporta requests capturados no formato HAR 1.2 (metadata only,
// sem bodies — economia de tokens, MELHORIAS §2).
package har

import (
	"encoding/json"
	"time"

	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harRequest struct {
	Method      string      `json:"method"`
	URL         string      `json:"url"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     []harHeader `json:"headers"`
	BodySize    int64       `json:"bodySize"`
}

type harResponse struct {
	Status      int         `json:"status"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     []harHeader `json:"headers"`
	BodySize    int64       `json:"bodySize"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type harDoc struct {
	Log harLog `json:"log"`
}

// WriteHAR serializa reqs como HAR 1.2. Bodies NAO sao incluidos (so bodySize).
// ponytail: Time fica 0 (nao temos duracao por request); preencher se o capture passar a medir.
func WriteHAR(target string, reqs []*store.Request) ([]byte, error) {
	entries := make([]harEntry, 0, len(reqs))
	for _, r := range reqs {
		entries = append(entries, harEntry{
			StartedDateTime: time.UnixMilli(r.Ts).UTC().Format(time.RFC3339),
			Request: harRequest{Method: r.Method, URL: r.URL, HTTPVersion: "HTTP/1.1",
				Headers: flatten(r.ReqHeaders), BodySize: int64(len(r.ReqBody))},
			Response: harResponse{Status: r.Status, HTTPVersion: "HTTP/1.1",
				Headers: flatten(r.RespHeaders), BodySize: int64(r.RespLen)},
		})
	}
	return json.MarshalIndent(harDoc{Log: harLog{Version: "1.2",
		Creator: harCreator{Name: "mcp-proxy", Version: "v5.1"}, Entries: entries}}, "", " ")
}

func flatten(hs map[string][]string) []harHeader {
	out := make([]harHeader, 0, len(hs))
	for k, vs := range hs {
		for _, v := range vs {
			out = append(out, harHeader{Name: k, Value: v})
		}
	}
	return out
}
