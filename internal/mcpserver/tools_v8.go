package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/isaiaspereira307/network-copitlot/internal/decoder"
	"github.com/isaiaspereira307/network-copitlot/internal/extensions"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

func registerV8Tools(s *Server) {
	s.tools["decode"] = s.toolDecode
	s.tools["encode"] = s.toolEncode
	s.tools["compare"] = s.toolCompare
	s.tools["tag_request"] = s.toolTagRequest
	s.tools["add_comment"] = s.toolAddComment
	s.tools["list_tags"] = s.toolListTags
	s.tools["ext_list"] = s.toolExtList
	s.tools["ext_enable"] = s.toolExtEnable
	s.tools["ext_disable"] = s.toolExtDisable
}

// ensureExtensions inicializa o manager apontando para o workspace do projeto.
func (s *Server) ensureExtensions() *extensions.Manager {
	if s.extMgr == nil {
		ws := ""
		if p, _ := s.active.Project(); p != nil {
			ws = p.Dir(s.repo.WorkspacePath())
		}
		s.extMgr = extensions.New(ws)
	}
	return s.extMgr
}

// toolDecode decodifica input num formato (base64, url, hex, html, jwt, gzip).
func (s *Server) toolDecode(ctx context.Context, args map[string]any) (string, error) {
	format := argStr(args, "format")
	input := argStr(args, "input")
	if format == "" || input == "" {
		return "", fmt.Errorf("format e input obrigatorios (formats: %s)", strings.Join(decoder.Formats, ", "))
	}
	out, err := decoder.Decode(format, input)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{"format": format, "decoded": out})
	return string(b), nil
}

// toolEncode codifica input num formato.
func (s *Server) toolEncode(ctx context.Context, args map[string]any) (string, error) {
	format := argStr(args, "format")
	input := argStr(args, "input")
	if format == "" || input == "" {
		return "", fmt.Errorf("format e input obrigatorios (formats: %s)", strings.Join(decoder.Formats, ", "))
	}
	out, err := decoder.Encode(format, input)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{"format": format, "encoded": out})
	return string(b), nil
}

// toolCompare diff visual de 2 requests/responses (mode resp|req|headers).
func (s *Server) toolCompare(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo")
	}
	left, ok1 := argInt(args, "left_id")
	right, ok2 := argInt(args, "right_id")
	if !ok1 || !ok2 {
		return "", fmt.Errorf("left_id e right_id obrigatorios")
	}
	kind := argStr(args, "kind")
	if kind == "" {
		kind = "response"
	}
	mode := map[string]string{"response": "resp", "request": "req"}[kind]
	if mode == "" && kind != "headers" {
		mode = "resp"
	}
	if kind == "headers" {
		mode = "headers"
	}
	d, err := st.DiffRequests(left, right, mode)
	if err != nil {
		return "", err
	}
	// frugal: retorna resumo + amostra de linhas (max 60)
	trunc := d.Lines
	if len(trunc) > 60 {
		trunc = trunc[:60]
	}
	out := map[string]any{
		"mode": d.Mode,
		"changed_left": len(d.ChangedAB), "changed_right": len(d.ChangedBA),
		"lines": trunc, "total_lines": len(d.Lines),
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// toolTagRequest adiciona uma tag a um request (Logger++).
func (s *Server) toolTagRequest(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo")
	}
	rid, ok := argInt(args, "request_id")
	if !ok {
		return "", fmt.Errorf("request_id obrigatorio")
	}
	tag := argStr(args, "tag")
	if tag == "" {
		return "", fmt.Errorf("tag obrigatoria")
	}
	r, err := st.Get(rid)
	if err != nil {
		return "", err
	}
	tags := r.Tags
	dup := false
	for _, t := range tags {
		if t == tag {
			dup = true
		}
	}
	if !dup {
		tags = append(tags, tag)
	}
	if err := st.SetRequestTags(rid, tags); err != nil {
		return "", err
	}
	return fmt.Sprintf("request %d tagged %q", rid, tag), nil
}

// toolAddComment anexa um comentario a um request (Logger++).
func (s *Server) toolAddComment(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo")
	}
	rid, ok := argInt(args, "request_id")
	if !ok {
		return "", fmt.Errorf("request_id obrigatorio")
	}
	comment := argStr(args, "comment")
	if comment == "" {
		return "", fmt.Errorf("comment obrigatorio")
	}
	if err := st.AddRequestNote(rid, comment); err != nil {
		return "", err
	}
	return fmt.Sprintf("comentario adicionado ao request %d", rid), nil
}

// toolListTags lista todas as tags em uso no alvo ativo.
func (s *Server) toolListTags(ctx context.Context, args map[string]any) (string, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return "", err
	}
	if st == nil {
		return "", fmt.Errorf("nenhum alvo ativo")
	}
	tags, err := st.ListTags()
	if err != nil {
		return "", err
	}
	out := map[string]any{"tags": tags, "count": len(tags)}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// projectName_ ativo (helper).
func (s *Server) projectName() string {
	if p, _ := s.active.Project(); p != nil {
		return p.Name
	}
	return ""
}

// toolExtList lista extensions conhecidas e seu status no projeto ativo.
func (s *Server) toolExtList(ctx context.Context, args map[string]any) (string, error) {
	mgr := s.ensureExtensions()
	out := map[string]any{"extensions": mgr.List(s.projectName())}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// toolExtEnable ativa uma extension no projeto ativo (allowlist).
func (s *Server) toolExtEnable(ctx context.Context, args map[string]any) (string, error) {
	ext := argStr(args, "ext_name")
	if ext == "" {
		return "", fmt.Errorf("ext_name obrigatorio")
	}
	if err := s.ensureExtensions().Enable(s.projectName(), ext); err != nil {
		return "", err
	}
	return fmt.Sprintf("extension %q habilitada no projeto", ext), nil
}

// toolExtDisable desativa uma extension no projeto ativo.
func (s *Server) toolExtDisable(ctx context.Context, args map[string]any) (string, error) {
	ext := argStr(args, "ext_name")
	if ext == "" {
		return "", fmt.Errorf("ext_name obrigatorio")
	}
	if err := s.ensureExtensions().Disable(s.projectName(), ext); err != nil {
		return "", err
	}
	return fmt.Sprintf("extension %q desabilitada no projeto", ext), nil
}

var _ = store.SevCrit
