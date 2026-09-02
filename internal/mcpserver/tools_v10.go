package mcpserver

import (
	"context"
	"fmt"
	"strings"
)

func registerV10Tools(s *Server) {
	s.tools["export_curl"] = s.toolExportCurl
	// Tasks 2-4: export_har, jwt_decode, jwt_resign
}

// toolExportCurl reconstrói um request capturado como comando curl pronto.
func (s *Server) toolExportCurl(ctx context.Context, args map[string]any) (string, error) {
	id, ok := argInt(args, "id")
	if !ok {
		return "", fmt.Errorf("id obrigatorio")
	}
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	r, err := st.Get(id)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "curl -X %s '%s'", r.Method, r.URL)
	for k, vs := range r.ReqHeaders {
		for _, v := range vs {
			fmt.Fprintf(&b, " -H '%s: %s'", k, v)
		}
	}
	if len(r.ReqBody) > 0 {
		if len(r.ReqBody) <= 200 {
			fmt.Fprintf(&b, " --data '%s'", string(r.ReqBody))
		} else {
			fmt.Fprintf(&b, " --data-binary @- <<'EOF'\n%s\nEOF", string(r.ReqBody))
		}
	}
	return b.String(), nil
}
