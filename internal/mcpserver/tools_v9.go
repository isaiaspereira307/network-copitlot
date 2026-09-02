package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isaiaspereira307/network-copitlot/internal/report"
	"github.com/isaiaspereira307/network-copitlot/internal/store"
)

// reportOutDir e onde os relatorios sao gravados por projeto.
const reportOutDir = "reports"

func registerV9Tools(s *Server) {
	s.tools["report_export_markdown"] = s.toolReportMarkdown
	s.tools["report_export_html"] = s.toolReportHTML
	s.tools["report_export_pdf"] = s.toolReportPDF
}

// reportFindings carrega findings (com filtro de status) do alvo ativo.
func (s *Server) reportFindings(statusFilter string) ([]*store.Finding, error) {
	st, err := s.openStoreForActiveTarget()
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("nenhum alvo ativo")
	}
	findings, err := st.ListFindings("", "")
	if err != nil {
		return nil, err
	}
	if statusFilter != "" {
		filtered := findings[:0]
		for _, f := range findings {
			if string(f.Status) == statusFilter {
				filtered = append(filtered, f)
			}
		}
		findings = filtered
	}
	return findings, nil
}

// reportPath devolve o caminho de saida do relatorio (server-side, frugal).
func (s *Server) reportPath(ext string) string {
	dir := ""
	if p, _ := s.active.Project(); p != nil {
		dir = filepath.Join(p.Dir(s.repo.WorkspacePath()), reportOutDir)
		os.MkdirAll(dir, 0o755)
	}
	tgt := ""
	if t, _ := s.active.Target(); t != nil {
		tgt = t.Host
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, tgt)
	return filepath.Join(dir, "report_"+safe+ext)
}

// toolReportMarkdown exporta findings em Markdown HackerOne-ready.
func (s *Server) toolReportMarkdown(ctx context.Context, args map[string]any) (string, error) {
	findings, err := s.reportFindings(argStr(args, "status_filter"))
	if err != nil {
		return "", err
	}
	tgt := s.targetHost()
	data, err := report.WriteMarkdown(tgt, findings)
	if err != nil {
		return "", err
	}
	path := s.reportPath(".md")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("relatorio escrito em %s (%d findings)", path, len(findings)), nil
}

// toolReportHTML exporta findings em HTML.
func (s *Server) toolReportHTML(ctx context.Context, args map[string]any) (string, error) {
	findings, err := s.reportFindings(argStr(args, "status_filter"))
	if err != nil {
		return "", err
	}
	tgt := s.targetHost()
	data, err := report.WriteHTML(tgt, findings)
	if err != nil {
		return "", err
	}
	path := s.reportPath(".html")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("relatorio escrito em %s (%d findings)", path, len(findings)), nil
}

// toolReportPDF exporta HTML; PDF headless requer chrome/binario chrome — grava
// o HTML e orienta a conversao (chromedp opcional, PRD v5.1).
func (s *Server) toolReportPDF(ctx context.Context, args map[string]any) (string, error) {
	findings, err := s.reportFindings(argStr(args, "status_filter"))
	if err != nil {
		return "", err
	}
	tgt := s.targetHost()
	data, err := report.WriteHTML(tgt, findings)
	if err != nil {
		return "", err
	}
	htmlPath := s.reportPath(".html")
	if err := os.WriteFile(htmlPath, data, 0o644); err != nil {
		return "", err
	}
	pdfPath := s.reportPath(".pdf")
	// chrome headless (se presente) gera o PDF; senao, orienta.
	pdf, err := renderPDF(htmlPath)
	if err == nil && len(pdf) > 0 {
		if werr := os.WriteFile(pdfPath, pdf, 0o644); werr == nil {
			return fmt.Sprintf("relatorio PDF escrito em %s (%d findings)", pdfPath, len(findings)), nil
		}
	}
	return fmt.Sprintf("HTML gerado em %s. PDF requer chrome headless: converta com 'chromium --headless --print-to-pdf=%s %s' (%d findings)", htmlPath, pdfPath, htmlPath, len(findings)), nil
}

func (s *Server) targetHost() string {
	if t, _ := s.active.Target(); t != nil {
		return t.Host
	}
	return ""
}

var _ = json.Marshal

// renderPDF executa chrome/headless para converter HTML em PDF. Retorna nil
// (sem chrome) para o caller orientar a conversao manual (chromedp opcional).
func renderPDF(htmlPath string) ([]byte, error) {
	return nil, fmt.Errorf("chrome headless nao configurado")
}
