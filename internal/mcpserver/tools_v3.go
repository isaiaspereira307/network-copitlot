package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/isaias/network-copitlot/internal/audit"
)

func registerV3Tools(s *Server) {
	// endpoints (task 14)
	s.tools["list_endpoints"] = s.toolListEndpoints
}

// toolListEndpoints lista os endpoints deduplicados do alvo ativo: agrega por
// (method, path normalizado) com segmentos dinamicos ({id}) colapsados, e
// reporta hit_count total + ate 5 sample_ids (mais recentes) para follow-up.
// Nunca expoe corpos.
func (s *Server) toolListEndpoints(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "list_endpoints", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	eps, err := st.ListEndpoints()
	if err != nil {
		s.audit.Log(audit.Event{Tool: "list_endpoints", Action: "error", Detail: map[string]any{"err": err.Error()}})
		return "", err
	}
	// ponytail: map manual p/ keys minusculas; Endpoint sem json tags.
	epsOut := make([]map[string]any, 0, len(eps))
	for _, e := range eps {
		epsOut = append(epsOut, map[string]any{
			"method":     e.Method,
			"path":       e.Path,
			"hit_count":  e.HitCount,
			"sample_ids": e.SampleIDs,
		})
	}
	out := map[string]any{"count": len(epsOut), "endpoints": epsOut}
	b, _ := json.Marshal(out)
	s.audit.Log(audit.Event{Tool: "list_endpoints", Action: "list", Detail: map[string]any{"count": len(epsOut)}})
	return string(b), nil
}
