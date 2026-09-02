package mcpserver

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/isaiaspereira307/network-copitlot/internal/har"
)

func registerV10Tools(s *Server) {
	s.tools["export_curl"] = s.toolExportCurl
	s.tools["export_har"] = s.toolExportHAR
	// Tasks 3-4: jwt_decode, jwt_resign
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

// toolExportHAR exporta o alvo ativo inteiro como HAR 1.2 (metadata only,
// sem bodies) no diretorio de reports do projeto ativo.
func (s *Server) toolExportHAR(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo: selecione um alvo com set_active_target")
	}
	reqs, err := st.All()
	if err != nil {
		return "", err
	}
	raw, err := har.WriteHAR(s.targetHost(), reqs)
	if err != nil {
		return "", err
	}
	path := s.reportPath(".har")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("HAR exportado em %s (%d entries, metadata only)", path, len(reqs)), nil
}
