package store

import (
	"fmt"
	"sort"
	"strings"
)

// maxDiffInputLines limita a entrada do LCS por lado (trunca do fim). O custo
// O(n*m) e quadratico em memoria (tabela []int32); este e o freio de custo.
const maxDiffInputLines = 1500

// DiffRequests compara dois requests (a, b) conforme o mode e devolve um diff
// unificado minimal por linha (LCS, prefixo " "/"-"/"+"). Modos: resp (default,
// response body), req (request body), headers (cabecalhos req+resp como linhas).
func (s *SQLiteStore) DiffRequests(a, b int64, mode string) (*Diff, error) {
	ra, err := s.Get(a)
	if err != nil {
		return nil, fmt.Errorf("request %d: %w", a, err)
	}
	rb, err := s.Get(b)
	if err != nil {
		return nil, fmt.Errorf("request %d: %w", b, err)
	}
	switch mode {
	case "", "resp":
		return unifiedDiff("resp", splitLines(ra.RespBody), splitLines(rb.RespBody)), nil
	case "req":
		return unifiedDiff("req", splitLines(ra.ReqBody), splitLines(rb.ReqBody)), nil
	case "headers":
		return unifiedDiff("headers", headerLines(ra), headerLines(rb)), nil
	default:
		return nil, fmt.Errorf("mode invalido %q: use resp, req ou headers", mode)
	}
}

// unifiedDiff monta o diff unificado. ChangedAB = linhas so em a, ChangedBA =
// linhas so em b (sem prefixo), para resumos compactos sem reparse do diff.
func unifiedDiff(mode string, a, b []string) *Diff {
	ops := lcsOps(a, b)
	var lines, ab, ba []string
	var del, ins []string
	flush := func() {
		lines = append(lines, del...)
		lines = append(lines, ins...)
		del, ins = nil, nil
	}
	i, j := 0, 0
	for _, op := range ops {
		switch op {
		case -1:
			del = append(del, "-"+a[i])
			ab = append(ab, a[i])
			i++
		case 1:
			ins = append(ins, "+"+b[j])
			ba = append(ba, b[j])
			j++
		default:
			flush()
			lines = append(lines, " "+a[i])
			i++
			j++
		}
	}
	flush()
	return &Diff{Mode: mode, Lines: lines, ChangedAB: ab, ChangedBA: ba}
}

// lcsOps devolve a sequencia de operacoes (0=comum, -1=remove de a, +1=insere
// de b) do LCS por linha. DP O(n*m) em tabela []int32 com backtracking;
// empates preferem remocao (padrao de diff unificado).
func lcsOps(a, b []string) []int32 {
	if len(a) > maxDiffInputLines {
		a = a[len(a)-maxDiffInputLines:]
	}
	if len(b) > maxDiffInputLines {
		b = b[len(b)-maxDiffInputLines:]
	}
	n, m := len(a), len(b)
	width := m + 1
	dp := make([]int32, (n+1)*width)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i*width+j] = dp[(i+1)*width+(j+1)] + 1
			} else if dp[(i+1)*width+j] >= dp[i*width+(j+1)] {
				dp[i*width+j] = dp[(i+1)*width+j]
			} else {
				dp[i*width+j] = dp[i*width+(j+1)]
			}
		}
	}
	var ops []int32
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, 0)
			i++
			j++
		case dp[(i+1)*width+j] >= dp[i*width+(j+1)]:
			ops = append(ops, -1)
			i++
		default:
			ops = append(ops, 1)
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, -1)
	}
	for ; j < m; j++ {
		ops = append(ops, 1)
	}
	return ops
}

// headerLines serializa os cabecalhos req+resp em linhas "name: v1, v2"
// ordenadas por nome (case-insensitive) para um diff estavel.
func headerLines(r *Request) []string {
	var out []string
	for name, vs := range r.ReqHeaders {
		out = append(out, strings.ToLower(name)+": "+strings.Join(vs, ", "))
	}
	for name, vs := range r.RespHeaders {
		out = append(out, strings.ToLower(name)+": "+strings.Join(vs, ", "))
	}
	sort.Strings(out)
	return out
}

// splitLines quebra um body em linhas sem terminador; vazio -> nil.
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
