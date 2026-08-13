# Profissionalização do mcp-proxy (P0→P4) — Implementation Plan

> **Para agentes executores:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recomendado) ou superpowers:executing-plans para implementar este plano task-a-task. Steps usam checkbox (`- [ ]`) para tracking.

**Goal:** Transformar o `mcp-proxy` num assistente de pentest/CTF/bug bounty de primeira linha, com obsessão por economia de tokens dentro de janela de contexto finita — implementando as tools v1 que faltam (P0), dedup/diff/summarize (P1), scanner passivo + M&R + intruder + CTF (P2), robustez de segurança (P3) e ergonomia de IA (P4).

**Architecture:** Store SQLite ganha filtros, busca, replay, findings. Camada MCP expõe ~25 tools novas com descrições ricas em inglês e retornos sempre resumidos/truncados (nunca despejar bodies). Proxy ganha content-type gating + cap de body + Match&Replace hook + scope guard no replay. Scanner passivo roda em cima do tráfego já capturado (zero tráfego novo). Findings em tabela SQLite por alvo. Intruder com worker pool + resultados agregados.

**Tech Stack:** Go 1.25.5 · `elazarl/goproxy v1.8.5` · `mark3labs/mcp-go v0.57.0` · `spf13/cobra v1.10.2` · `modernc.org/sqlite v1.55.0` (puro Go, sem CGO) · `gopkg.in/yaml.v3` · stdlib `log`/`testing`/`httptest`/`net/http`/`crypto/tls`.

## Global Constraints

- **Go 1.25.5**, zero CGO, binário estático único, sem dependências de runtime novas.
- **TDD estrito**: cada task começa com teste falhando. Framework = stdlib `testing` + `httptest`. Sem testify/gomock.
- **Descrições de tool MCP em inglês, ricas, com dicas de paginação/truncamento.** Retornos sempre JSON estruturado e estável.
- **Nunca retornar bodies inteiros por padrão.** `list_*` retornam só metadados; `get_request_detail` truncar por default (`max_body_bytes=8192`, `body_range?`), sinalizar `body_truncated: true, total_len: N`.
- **Replay herda scope guard**: host fora de escopo → recusa na tool, não confiar na IA.
- **`add_target --confirm`** mantém obrigatório (CLI + MCP). Scope só mutável via `set_scope` (nova tool) ou recriação.
- **Banco SQLite por target**: `<workspace>/<project>/targets/<host>/requests.db` (schema atual) + nova tabela `findings` adicionada (não substituída).
- **Audit log**: continua `~/.mcp-proxy/audit.log`, redação só em chaves sensíveis; **documentar que `requests.db` contém segredos em claro** (threat model) — não redigir bodies (decisão de design de pentest).
- **Sem novas deps grandes**: FTS5 adiável (SQLite embute; só ativar se `LIKE` medir lento). `modernc.org/sqlite` já suporta.
- **Commits pequenos por task**, Conventional Commits: `feat(scope): ...`, `fix(scope): ...`, `docs: ...`, `test: ...`.

## File Structure

**Modificações:**
- `internal/store/store.go` — estender interface `Store` (filtros, busca, replay, findings, endpoints).
- `internal/store/sqlite.go` — implementar novos métodos + queries parametrizadas (substituir `fmt.Sprintf`).
- `internal/store/schema.go` — adicionar tabela `findings` + índices + coluna `interesting`.
- `internal/proxy/proxy.go` — content-type gating, cap de body (`io.LimitReader`), hook M&R, `InsecureSkipVerify` via config.
- `internal/proxy/scope.go` — exportar `Scope` e `Matches` para reuse no replay.
- `internal/projects/repo.go` — adicionar `UpdateTarget` (set_scope) e `SetScope`.
- `internal/projects/model.go` — adicionar `MatchReplaceRule`, `Finding` structs.
- `internal/mcpserver/server.go` — registrar ~25 tools novas + descrições em inglês.
- `internal/mcpserver/tools_v2.go` — adicionar handlers (ou split em `tools_v3.go`, `tools_v4.go` por sprint).
- `internal/config/config.go` — adicionar `Proxy` (body cap, content-type gating, strict upstream) + `Audit.MaxSizeMB`.
- `cmd/mcp-proxy/main.go` — warnings de startup, rotation audit, flags `--body-cap`, `--strict-upstream`, `--no-body-content-types`.
- `README.md` — corrigir alegação falsa + guia do operador + system prompt sugerido.
- `MELHORIAS.md` — marcar blocos entregues (checklist no fim de cada sprint).

**Criações:**
- `internal/store/findings.go` — repositório de findings.
- `internal/store/endpoints.go` — agrupamento `(method, path-normalizado)`.
- `internal/store/replay.go` — reexecução HTTP com scope guard.
- `internal/store/diff.go` — diff unificado de responses.
- `internal/mcpserver/tools_v3.go` — tools P1 (list_endpoints, diff_requests, summarize_response, anomaly hints).
- `internal/mcpserver/tools_v4.go` — tools P2 (M&R, scanner, intruder, sitemap, CTF exports).
- `internal/scanner/scanner.go` — worker pool passivo + detectores.
- `internal/scanner/detect.go` — detectores XSS/IDOR/SQLi/SSRF/secret.
- `internal/intruder/intruder.go` — engine de fuzzing.
- `internal/summarize/summarize.go` — extração HTML/JS/JSON.
- `internal/matchreplace/matchreplace.go` — regras M&R + apply em `onRequest`/`onResponse`.
- `docs/threat-model.md` — modelo de ameaça expandido (segredos em claro, opt-in ativo).
- `docs/operator-guide.md` — guia "como pedir para a IA" (exemplos).

**Testes:** cada pacote novo traz `<pkg>_test.go`. `test/e2e/` ganha `p0_tools_test.go`, `p1_economy_test.go`, `p2_pentest_test.go`.

---

## SPRINT 1 — P0: tornar o produto funcional (~1 dia)

### Task 1: Corrigir README — remover alegação falsa de v1 entregue

**Files:**
- Modify: `README.md:243-256` (seção Roadmap), `README.md:122-136` (tabela de tools)

**Steps:**
- [ ] **1.1** Substituir tabela de tools do README (linhas 122-136) pela lista **real**: 7 tools de gestão v2. Remover `list_requests`, `get_request_detail`, `replay_request`, `set_scope`, `search_bodies` dessa tabela.
- [ ] **1.2** Na seção Roadmap (linha 247), trocar `✅ v1: ... 5 MCP tools` por `⚠️ v1 (parcial): proxy MITM + CA + storage; tools de leitura/replay pendentes — ver Sprint 1 do plano`.
- [ ] **1.3** Commit: `docs: corrigir alegação falsa de tools v1 entregues`

### Task 2: Estender a interface `Store` com filtros, busca, replay, findings

**Files:**
- Modify: `internal/store/store.go` (interface + tipos)
- Test: `internal/store/store_test.go` (extender `stubStore` para assinar nova interface)

**Interfaces (Produces):**
```go
type ListFilter struct {
    Limit        int
    Offset       int
    MethodFilter string
    StatusFilter int    // 0 = any
    HostFilter   string
    PathContains string
    SinceID      int64
    Interesting  bool   // só marcados (P1)
}

type RequestSummary struct {
    ID      int64
    Ts      int64
    Method  string
    URL     string
    Status  int
    RespLen int
}

type RequestDetail struct {
    ID                int64
    Ts                int64
    Method            string
    URL               string
    ReqHeaders        map[string][]string
    ReqBody           []byte
    Status            int
    RespHeaders       map[string][]string
    RespBody          []byte
    RespLen           int
    ReqBodyTruncated  bool
    RespBodyTruncated bool
    ReqTotalLen       int
    RespTotalLen      int
}

// All streams every captured request WITH bodies (req+resp), for scanner/export.
func (s Store) All() ([]*Request, error)

type BodyMatch struct {
    ID           int64
    URL          string
    MatchSnippet string
}

type ReplayOverrides struct {
    HeaderOverrides map[string]string
    BodyOverride    []byte
    MethodOverride  string
    URLOverride     string
    FollowRedirects bool
}

type ReplayResult struct {
    NewRequestID int64
    Status       int
    RespLen      int
}

type Endpoint struct {
    Method    string
    Path      string
    HitCount  int
    SampleIDs []int64
}

type Diff struct {
    Mode      string
    Lines     []string
    ChangedAB []string
    ChangedBA []string
}

func (s Store) List(f ListFilter) ([]*RequestSummary, error)
func (s Store) GetDetail(id int64, include string, maxBody int, bodyRange string) (*RequestDetail, error)
func (s Store) SearchBodies(pattern string, scope string, limit int) ([]*BodyMatch, error)
func (s Store) Replay(id int64, overrides ReplayOverrides, scopeMatch func(string) bool) (*ReplayResult, error)
func (s Store) ListEndpoints() ([]*Endpoint, error)
func (s Store) DiffRequests(a, b int64, mode string) (*Diff, error)
func (s Store) All() ([]*Request, error)
```

- [ ] **2.1** Adicionar tipos `RequestSummary`, `RequestDetail`, `BodyMatch`, `ReplayOverrides`, `ReplayResult`, `Endpoint`, `Diff` ao `store.go`.
- [ ] **2.2** Escrever teste `TestStoreInterface_Compiles_NewMethods` que chama cada novo método do `stubStore` — falha: não definido.
- [ ] **2.3** Adicionar signatures na interface `Store` (incluindo `All() ([]*Request, error)`, decisão aprovada 2026-08-12) + stub no-op no `stubStore`.
- [ ] **2.4** Commit: `feat(store): estender interface com List filtros, SearchBodies, Replay, Diff, ListEndpoints`

### Task 3: Implementar `List` com filtros e `GetDetail` com truncamento (sqlite.go)

**Files:**
- Modify: `internal/store/sqlite.go:84-105`
- Test: `internal/store/sqlite_test.go`

**Consumes:** `ListFilter` (Task 2). **Produces:** `[]*RequestSummary` (id, ts, method, url, status, resp_len).

- [ ] **3.1** Teste `TestSQLiteStore_List_FiltersByMethodStatusHost` falha. Insere 5 requests variados; chama `List(ListFilter{MethodFilter:"POST",StatusFilter:200})`; espera 1.
- [ ] **3.2** Implementar: query parametrizada `SELECT id,ts,method,url,status,resp_len FROM requests WHERE 1=1 [AND method=?] [AND status=?] ... ORDER BY id DESC LIMIT ? OFFSET ?`. Substituir `fmt.Sprintf` atual por placeholders.
- [ ] **3.3** Teste passa.
- [ ] **3.4** Teste `TestSQLiteStore_GetDetail_TruncatesBody` falha: insere request com body de 20KB; pede `maxBody=8192`; espera `BodyTruncated=true`, `TotalLen=20480`, `Body` com 8192 bytes.
- [ ] **3.5** Implementar `GetDetail` com `body_range` parse (`"0-4096"` → `body[start:end]`).
- [ ] **3.6** Commit: `feat(store): List com filtros + GetDetail com truncamento orçado`

### Task 4: `SearchBodies` (LIKE substring + regex Go-side)

**Files:**
- Modify: `internal/store/sqlite.go`
- Test: `internal/store/sqlite_test.go`

- [ ] **4.1** Teste `TestSearchBodies_SubstringAndRegex` falha: insere 3 com `Authorization: Bearer X`, `SELECT * FROM`, `q=reflected`; busca `Bearer`; busca regex `SELECT\s+\*`.
- [ ] **4.2** Implementar: `SELECT id,url,req_body,resp_body FROM requests` (limit interno 500) → Go filter por substring (`bytes.Contains`) ou regex (`regexp.Compile`) no `scope` (req/resp/both); extrai `match_snippet` (±80 chars ao redor); retorna `[]*BodyMatch{id,url,match_snippet}`.
- [ ] **4.3** Commit: `feat(store): SearchBodies substring+regex com snippet`

### Task 5: `Replay` com scope guard e gravação do replay no store

**Files:**
- Create: `internal/store/replay.go`
- Modify: `internal/store/sqlite.go` (register method na interface)
- Test: `internal/store/replay_test.go` (httptest upstream)

**Consumes:** `scopeMatches func(host string) bool` injetado pela tool (de `proxy.scope` exportado na Task 7). **Produces:** `ReplayResult{NewRequestID, Status, RespLen}`.

- [ ] **5.1** Teste `TestReplay_HonorsScopeGuard` falha: upstream httptest; replay com `urlOverride` para host fora de escopo → erro `ErrOutOfScope`. Host dentro → 200, novo id gravado.
- [ ] **5.2** Implementar: parse overrides (header/body/method/url/follow_redirects); montar `http.NewRequestWithContext`; se `urlOverride` vazio, usar URL original; **checar `scopeMatches(u.Hostname())` antes de enviar**; executar via `http.Client{}` com transporte TLS `InsecureSkipVerify: true` hardcoded (decisão aprovada 2026-08-12: `store.Replay` não acessa o proxy; revisitado na Task 26 quando strict upstream virar config); gravar como `Request` novo; retornar `{NewRequestID, Status, RespLen}`.
- [ ] **5.3** Commit: `feat(store): Replay com scope guard + persistência do replay`

### Task 6: `UpdateTarget`/`SetScope` no `projects.Repo`

**Files:**
- Modify: `internal/projects/repo.go`, `internal/projects/model.go`
- Modify: `internal/projects/repo_test.go`

- [ ] **6.1** Teste `TestRepo_UpdateTarget_MergesScope` falha: cria target, atualiza `InScopePatterns` para `["*.corp"]`, recarrega, confere.
- [ ] **6.2** Implementar `(*Repo).UpdateTarget(projectName string, t *Target) error` — read-modify-write de `meta.yaml` (preserva `Notes`, atualiza `InScope/OutOfScopePatterns`).
- [ ] **6.3** Wrapper `(*Repo).SetScope(projectName, host string, inScope, outOfScope []string) error`.
- [ ] **6.4** Commit: `feat(projects): UpdateTarget + SetScope para set_scope`

### Task 7: Exportar `scope.Matches` do proxy e expor helper no mcpserver

**Files:**
- Modify: `internal/proxy/scope.go` (exportar `Scope` struct e `Matches`)
- Modify: `internal/proxy/proxy.go:56-61` (uso interno atualiza nome)
- Modify: `internal/proxy/scope_test.go`
- Modify: `internal/mcpserver/server.go` (reuse em `replay_request`)

- [ ] **7.1** Teste `TestScope_ExportedMatches` falha (renomeia).
- [ ] **7.2** `type Scope struct {...}` + `func New(t *projects.Target) *Scope` + `func (s *Scope) Matches(u *url.URL) bool`. Interno vira thin wrapper.
- [ ] **7.3** Commit: `refactor(proxy): exportar Scope.Matches para reuse no replay`

### Task 8: Registrar MCP tool `list_requests`

**Files:**
- Modify: `internal/mcpserver/server.go` (AddTool), `tools_v2.go` (handler)
- Test: `internal/mcpserver/tools_v2_test.go` (extender `callTool`)

**Consumes:** `store.List` (Task 3). **Description (EN, rica):** `"List captured requests paginated by recency. Returns ONLY summary fields (id, ts, method, url, status, resp_len) — never bodies. Use filters to narrow; default limit=50. Page with offset/since_id. Pick interesting ids then call get_request_detail."`

- [ ] **8.1** Teste `TestListRequestsTool_ReturnsOnlySummary` falha: insere 3 requests via store direto; chama tool; espera keys `id,ts,method,url,status,resp_len` e **ausência** de `req_body`.
- [ ] **8.2** Implementar handler `s.toolListRequests` + `s.mcp.AddTool(mcp.NewTool("list_requests", ...))`.
- [ ] **8.3** Commit: `feat(mcp): tool list_requests resumida e paginada`

### Task 9: Registrar MCP tool `get_request_detail` (com truncamento)

- [ ] **9.1** Teste `TestGetRequestDetail_TruncatesByDefault` falha.
- [ ] **9.2** Handler `s.toolGetRequestDetail` usando `store.GetDetail(id, include, maxBody, bodyRange)`.
- [ ] **9.3** Commit: `feat(mcp): tool get_request_detail com truncamento orçado`

### Task 10: Registrar MCP tool `search_bodies`

- [ ] **10.1** Teste `TestSearchBodiesTool_ReturnsSnippetNotBody` falha.
- [ ] **10.2** Handler `s.toolSearchBodies`.
- [ ] **10.3** Commit: `feat(mcp): tool search_bodies com snippet ±80 chars`

### Task 11: Registrar MCP tool `replay_request` (com scope guard)

- [ ] **11.1** Teste `TestReplayTool_RejectsOutOfScope` falha.
- [ ] **11.2** Handler `s.toolReplayRequest` — obtém target ativo, constrói `scope.New(target).Matches`, passa para `store.Replay`.
- [ ] **11.3** Commit: `feat(mcp): tool replay_request com scope guard`

### Task 12: Registrar MCP tool `set_scope`

- [ ] **12.1** Teste `TestSetScopeTool_PersistsAndApplies` falha: chama tool com `in_scope=["*.corp"]`, recarrega target, confere `InScopePatterns`.
- [ ] **12.2** Handler `s.toolSetScope` usando `repo.SetScope`.
- [ ] **12.3** Proxy vivo: mtime-check do meta.yaml a cada request (cache com stat barato) — recarrega scope via `SetTarget`-equivalente quando o arquivo mudou (decisão aprovada 2026-08-12: MCP server e proxy são processos separados; sem comunicação inter-processo).
- [ ] **12.3** Commit: `feat(mcp): tool set_scope persistente`

### Task 13: e2e Sprint 1 — fluxo completo browse→list→search→detail→replay

**Files:** Create `test/e2e/p0_tools_test.go`

- [ ] **13.1** Teste `TestE2E_P0_Tools` acessa upstream via proxy, captura 3 requests, chama tools via `callTool`, asserções em cada etapa. Commit: `test(e2e): P0 tools flow`

---

## SPRINT 2 — P1: economia de tokens

### Task 14: `list_endpoints` (dedup + normalização de path)

**Files:** Create `internal/store/endpoints.go`, `internal/mcpserver/tools_v3.go`

- Regex normaliza `/users/123` → `/users/{id}` (numérico, UUID, hex≥16, base64≥22).
- [ ] **14.1** Teste `TestListEndpoints_NormalizesIDs` falha.
- [ ] **14.2** Implementar: `GROUP BY normalized_path, method` em SQL com normalização Go pré-group.
- [ ] **14.3** Tool MCP `list_endpoints` retorna `{method, path, hit_count, sample_ids[]}`.
- [ ] **14.4** Commit: `feat(store,mcp): list_endpoints com normalização de path`

### Task 15: `diff_requests`

**Files:** Create `internal/store/diff.go`, Modify `tools_v3.go`

- [ ] **15.1** Teste `TestDiffRequests_BodyUnified` falha.
- [ ] **15.2** Implementar diff unificado minimal puro stdlib (LCS por linha, prefix `+/-/ `).
- [ ] **15.3** Tool `diff_requests(id_a, id_b, mode)`.
- [ ] **15.4** Commit: `feat(store,mcp): diff_requests com diff unificado compacto`

### Task 16: `summarize_response` (HTML/JSON/JS)

**Files:** Create `internal/summarize/summarize.go` + test

- [ ] **16.1** Teste `TestSummarize_HTML_ExtractsFormsAndLinks` falha.
- [ ] **16.2** Implementar: `golang.org/x/net/html` tokenizer (já indireto via goproxy) — extrai forms/links/scripts/comments do HTML; JSON → walk keys+tipos (sem valores) até profundidade 3; JS → regex por endpoints URLs/tokens + `fetch`/`XHR`.
- [ ] **16.3** Tool `summarize_response(id)`.
- [ ] **16.4** Commit: `feat(summarize,mcp): summarize_response HTML/JSON/JS`

### Task 17: Content-type gating + cap de body no proxy

**Files:** Modify `internal/proxy/proxy.go:180-187, 206`, `internal/config/config.go`, `cmd/mcp-proxy/main.go`

- [ ] **17.1** Teste `TestProxy_SkipsBodyForImages` falha: upstream serve `Content-Type: image/png` com 100KB; captura; store; body deve estar **vazio** com flag `body_skipped` (ou truncado).
- [ ] **17.2** Config `Proxy.NoBodyContentTypes []string` (default `image/*,font/*,video/*,text/css`) + `Proxy.BodyCapBytes int64` (default 1MB).
- [ ] **17.3** `io.LimitReader(resp.Body, cap)` + flag de truncamento. Pular body se Content-Type casar glob.
- [ ] **17.4** CLI flags `--body-cap`, `--no-body-content-types`.
- [ ] **17.5** Commit: `feat(proxy,config): content-type gating + cap de body`

### Task 18: Anomaly hints + coluna `interesting`

**Files:** Modify `internal/store/schema.go` (ADD COLUMN `interesting INTEGER DEFAULT 0`), `sqlite.go` (Insert computa hint), `tools_v3.go` (`list_requests?interesting=true`)

- Heurísticas: status 5xx, body contém `error|exception|stack trace`, headers debug `X-Powered-By|Server|X-Debug`, `Set-Cookie` sem `HttpOnly`/`Secure`.
- [ ] **18.1** Migração `ALTER TABLE` idempotente.
- [ ] **18.2** Teste `TestInteresting_Highlights5xxAndDebugHeaders`.
- [ ] **18.3** Commit: `feat(store,mcp): anomaly hints coluna interesting`

---

## SPRINT 3 — P2: capacidades de pentest

### Task 19: Match & Replace — rules + hook no proxy

**Files:** Create `internal/matchreplace/matchreplace.go`, Modify `internal/proxy/proxy.go` (chamada em `onRequest`/`onResponse`), `tools_v4.go`

- [ ] **19.1** Tipo `Rule{ID, Scope, Where, Match, Replace, Regex, Enabled}`. Store em `<project>/meta.yaml` (global) ou `<target>/meta.yaml`.
- [ ] **19.2** Teste `TestMatchReplace_ReplacesHeaderInFlight`.
- [ ] **19.3** Tools `save_mr_rule`, `list_mr_rules`, `toggle_mr_rule`.
- [ ] **19.4** Hook apply no `onRequest` (where=header|param|body) e `onResponse` (where=header|body).
- [ ] **19.5** Commit: `feat(matchreplace): regras M&R + tools + hook no proxy`

### Task 20: Scanner passivo — findings table + worker pool

**Files:** Create `internal/scanner/scanner.go`, `internal/scanner/detect.go`, Modify `internal/store/schema.go` (tabela `findings`), `store/findings.go`, `tools_v4.go`

- Schema `findings` (id, ts, type, severity, evidence JSON, status, notes).
- Detectores: XSS refletido (param verbatim em body), IDOR (id numérico/UUID em path/param; delta de tamanho entre IDs próximos), SQLi (error patterns), SSRF (param=URL + IPs privados/metadata na response), open redirect, secrets em JS (regex AWS/GitHub/JWT/Google).
- [ ] **20.1** Teste `TestScanner_DetectsReflectedXSS` falha.
- [ ] **20.2** Implementar worker pool + canais; itera sobre `store.All()` (decisão aprovada 2026-08-12: `List` retorna summaries sem bodies; scanner precisa de `All()`).
- [ ] **20.3** Tools `scan_passive_run`, `scan_passive_status`, `list_findings`, `get_finding_detail`, `finding_set_status`.
- [ ] **20.4** Commit: `feat(scanner): scanner passivo + findings + tools`

### Task 21: Intruder (fuzzer) com resultados agregados

**Files:** Create `internal/intruder/intruder.go`, Modify `tools_v4.go`

- 4 attack types (Sniper, Battering Ram, Pitchfork, Cluster Bomb). Throttle `req/s`. Resultados agregados por `(status, resp_len)`, não request-a-request.
- [ ] **21.1** Teste `TestIntruder_ClusterBomb_AggregatesByStatus`.
- [ ] **21.2** Persistir em `intruder/jobs/<id>/results.json`.
- [ ] **21.3** Tools `intruder_start/status/results/cancel`.
- [ ] **21.4** Commit: `feat(intruder): fuzzer agregado + tools`

### Task 22: Sitemap

**Files:** Modify `internal/store/endpoints.go` (tree view), `tools_v4.go`

- [ ] **22.1** Tool `get_sitemap` — consome `ListEndpoints` e monta árvore aninhada.
- [ ] **22.2** Teste `TestGetSitemap_BuildsTree`.
- [ ] **22.3** Commit: `feat(mcp): get_sitemap árvore de endpoints`

### Task 23: Decoder/comparer (mínimo, CTF)

**Files:** Create `internal/decoder/decoder.go`, `tools_v4.go`

- Encodes/decodes: base64, URL, hex, JWT (decode-only), HTML entities, gzip. **Não** adicionar lib nova — stdlib cobre tudo (`encoding/base64`, `net/url`, `encoding/hex`, `compress/gzip`, strings.Replace para entities).
- [ ] **23.1** Teste `TestDecoder_Base64Roundtrip`.
- [ ] **23.2** Tools `decode`, `encode`, `compare` (reusa `store.DiffRequests`).
- [ ] **23.3** Commit: `feat(decoder,mcp): decode/encode/compare`

### Task 24: Macros/sessão (mínimo)

**Files:** Create `internal/macro/macro.go`, `tools_v4.go`

- [ ] **24.1** Tipo `Macro{Name, Steps[]Request, ExtractRegex map[string]string}`. Persistir `macros/<name>.json`.
- [ ] **24.2** Tools `macro_record` (inicia captura p/ próximas N requests), `macro_play`, `macro_list`.
- [ ] **24.3** Commit: `feat(macro): macro_record/play/list`

### Task 25: CTF exports — JWT, cURL, HAR

**Files:** Create `internal/ctf/ctf.go`, `tools_v4.go`

- [ ] **25.1** `jwt_decode` (split por `.` + base64url header/payload; sem validar assinatura), `jwt_resign` (alg=none, key confusion — só re-encode com header trocado).
- [ ] **25.2** `export_curl(id)` — monta one-liner `curl -X METHOD -H ... --data ... URL`.
- [ ] **25.3** `export_har(target)` — JSON HAR 1.2 de `store.All()` (decisão aprovada 2026-08-12: HAR precisa de headers/bodies completos).
- [ ] **25.4** Commit: `feat(ctf): jwt_decode/resign + export_curl + export_har`

---

## SPRINT 4 — P3: segurança e robustez

### Task 26: `InsecureSkipVerify` via config + warning de startup alto

**Files:** Modify `internal/config/config.go`, `internal/proxy/proxy.go:113-118`, `cmd/mcp-proxy/main.go`

- [ ] **26.1** Config `Proxy.StrictUpstream bool` (default `false` = mantém comportamento pentest). CLI `--strict-upstream`.
- [ ] **26.2** Startup log: `"WARNING: strict_upstream=false — TLS do upstream NÃO validado (modo pentest). Próprio para alvos com cert quebrado."` em stderr com prefixo `WARNING`.
- [ ] **26.3** Commit: `feat(config,proxy): strict upstream configurável + warning`

### Task 27: Rotação do audit.log por tamanho

**Files:** Modify `internal/audit/audit.go`

- [ ] **27.1** Teste `TestAuditLog_RotatesAtSize` falha (escreve 2MB com `MaxSize=1MB`, espera `audit.log.1` criado).
- [ ] **27.2** Config `Audit.MaxSizeMB int` (default 10). Após cada write, se `>MaxSize`, rename para `audit.log.<ts>` e reabre.
- [ ] **27.3** Commit: `feat(audit): rotação por tamanho`

### Task 28: Documentar threat model (segredos em claro no DB)

**Files:** Create `docs/threat-model.md`, Modify `README.md` (link)

- [ ] **28.1** Seções: (a) `requests.db`/`findings.db` contêm segredos em claro — **pré-requisito**: `chmod 600` no workspace; backups auto-do-usuário. (b) CA privada `0600` nunca commitada. (c) Opt-in duplo para scanner ativo (futuro). (d) Replay respeita scope. (e) Audit redige chaves sensíveis mas não bodies.
- [ ] **28.2** Commit: `docs: threat-model com segredos em claro`

### Task 29: Migration `ALTER TABLE` idempotente (interesting + findings)

**Files:** Modify `internal/store/schema.go`, `internal/store/migrate.go`

- [ ] **29.1** Check via `PRAGMA table_info` antes de `ALTER TABLE` (SQLite não tem `IF NOT EXISTS` p/ coluna).
- [ ] **29.2** Teste `TestMigrate_AddsFindingsAndInterestingColumns`.
- [ ] **29.3** Commit: `feat(store): migrations idempotentes p/ interesting + findings`

---

## SPRINT 5 — P4: ergonomia da IA

### Task 30: Tool descriptions em inglês ricas + dicas de paginação

**Files:** Modify `internal/mcpserver/server.go` (linhas 41-89) + todas as tools novas

- [ ] **30.1** Reescrever descrições das 7 tools v2 existentes em inglês com 2-4 frases cada: o que faz, quando usar, dicas (ex: `get_active_context`: "Use first to know project/target and request count before issuing scoped queries.").
- [ ] **30.2** Revisar descrições das tools novas P0-P2 com mesma riqueza + dicas de truncamento (`max_body_bytes`, `body_range`, `limit`).
- [ ] **30.3** Teste `TestToolDescriptions_AreEnglishAndRich` — valida strings mínimas presentes.
- [ ] **30.4** Commit: `feat(mcp): descriptions em inglês ricas`

### Task 31: `get_active_context` rico (briefing <500 tokens)

**Files:** Modify `internal/mcpserver/tools_v2.go:78-104`

- [ ] **31.1** Teste `TestGetActiveContext_ReturnsBriefing` falha: contagem por status (2xx/3xx/4xx/5xx), top 5 hosts por hits, flag `scope_defined: bool`.
- [ ] **31.2** Implementar agregação (novas queries `Count` por status + top hosts).
- [ ] **31.3** Commit: `feat(mcp): get_active_context briefing rico`

### Task 32: Guia do operador + system prompt sugerido

**Files:** Create `docs/operator-guide.md`, Modify `README.md` (bloco "How to ask the AI")

- [ ] **32.1** `docs/operator-guide.md` com 15 exemplos: "liste endpoints únicos", "diff request 40 e 41", "procure Authorization nos bodies", "replay o request 12 com header X-Admin: 1", "rode scanner passivo e liste findings high".
- [ ] **32.2** README bloco + JSON do `mcpServers` com comentário de `system prompt` sugerido ("Sempre paginate. Use `search_bodies` antes de `get_request_detail`. Nunca peça body full sem `body_range`.").
- [ ] **32.3** Commit: `docs: operator-guide + system prompt sugerido`

### Task 33: Atualizar README roadmap com sprints entregues

**Files:** Modify `README.md:243-256`

- [ ] **33.1** Reverter "⚠️ v1 parcial" para "✅ v2.x: P0 tools + P1 economia + P2 pentest + P3 segurança + P4 ergonomia" conforme cada sprint fecha.
- [ ] **33.2** Commit final do master plan: `docs: roadmap atualizado pós-sprints`

---

## Self-Review (checklist rodado antes da execução)

**Spec coverage (MELHORIAS.md):**
- §1.1-1.5 (P0 tools) → Tasks 3,4,5,11,12 ✓
- §2.1-2.5 (P1) → Tasks 14,15,16,17,18 ✓
- §3 M&R/scanner/intruder/sitemap/decoder/macros → Tasks 19,20,21,22,23,24 ✓
- §3.1 CTF (JWT/curl/HAR) → Task 25 ✓
- §4 segurança achados → Tasks 26,27,28 + scope guard no replay (Task 11) ✓; achado #1 Run(ctx) aceitável documentado; #6 ECDSA baixa prioridade fora; #3 sem cap → Task 17 ✓
- §5 ergonomia → Tasks 30,31,32 ✓

**Gaps conhecidos:**
- Macros (Task 24) é o mais complexo; se sprint apertar, adiar para release pós-master sem bloquear o resto.
- FTS5 explicitamente adiado (MELHORIAS §1.3 "v futura"); LIKE + regex Go-side cobre.
- Scanner ativo + crawler (PRD v4.1) fora deste master plan — próximo plano se opt-in duplo.
- Extensions API Go plugins (v5) fora de escopo.
- Testes wire mcp-go fracos hoje (callTool bypassa SDK); não corrigiremos o framework de teste neste plano — risco aceito.

**Type consistency:** `RequestSummary`, `BodyMatch`, `ReplayResult`, `Endpoint`, `Diff`, `Finding`, `MatchReplaceRule`, `Macro` nomeados consistentemente Tasks 2→uso posterior.
