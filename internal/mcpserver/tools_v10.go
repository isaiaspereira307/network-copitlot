package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/isaiaspereira307/network-copitlot/internal/decoder"
	"github.com/isaiaspereira307/network-copitlot/internal/har"
)

func registerV10Tools(s *Server) {
	s.tools["export_curl"] = s.toolExportCurl
	s.tools["export_har"] = s.toolExportHAR
	s.tools["jwt_decode"] = s.toolJwtDecode
	s.tools["jwt_resign"] = s.toolJwtResign
}

// shellQuote envolve s em aspas simples com escaping POSIX sh (' -> '\''),
// impedindo que valores capturados quebrem o comando ou executem shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// heredocDelim devolve um delimitador que nenhuma linha do corpo contem.
func heredocDelim(id int64, body string) string {
	delim := fmt.Sprintf("EOF_%d", id)
	for n := 2; ; n++ {
		candidate := delim
		if n > 2 {
			candidate = fmt.Sprintf("%s_%d", delim, n)
		}
		if !lineExists(body, candidate) {
			return candidate
		}
	}
}

func lineExists(body, line string) bool {
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSuffix(l, "\r")
		if l == line {
			return true
		}
	}
	return false
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
	fmt.Fprintf(&b, "curl -X %s %s", shellQuote(r.Method), shellQuote(r.URL))
	for k, vs := range r.ReqHeaders {
		for _, v := range vs {
			fmt.Fprintf(&b, " -H %s", shellQuote(k+": "+v))
		}
	}
	if len(r.ReqBody) > 0 {
		if len(r.ReqBody) <= 200 {
			fmt.Fprintf(&b, " --data %s", shellQuote(string(r.ReqBody)))
		} else {
			delim := heredocDelim(id, string(r.ReqBody))
			fmt.Fprintf(&b, " --data-binary @- <<'%s'\n%s\n%s", delim, string(r.ReqBody), delim)
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

// toolJwtDecode decodifica um JWT completo (header+payload+sig) com warnings
// de superficie de ataque. Nao verifica assinatura (decode, nao verify).
func (s *Server) toolJwtDecode(ctx context.Context, args map[string]any) (string, error) {
	token := argStr(args, "token")
	if token == "" {
		return "", fmt.Errorf("token obrigatorio")
	}
	info, err := decoder.DecodeJWTFull(token)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// toolJwtResign re-assina um JWT offline (none/hs256) para acceptance
// testing. Nao envia trafego. Requer confirmed=true (padrao add_target).
func (s *Server) toolJwtResign(ctx context.Context, args map[string]any) (string, error) {
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return "", fmt.Errorf("confirmed=true obrigatorio")
	}
	token := argStr(args, "token")
	if token == "" {
		return "", fmt.Errorf("token obrigatorio")
	}
	alg := argStr(args, "alg")
	if alg == "" {
		return "", fmt.Errorf("alg obrigatorio: none ou hs256")
	}
	out, err := decoder.ResignJWT(token, alg, argStr(args, "secret"))
	if err != nil {
		return "", err
	}
	return out, nil
}
