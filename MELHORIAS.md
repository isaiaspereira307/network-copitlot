# mcp-proxy — Avaliação e Roadmap de Melhorias

> Objetivo: transformar o `mcp-proxy` num assistente de IA de primeira linha para
> **bug bounty, CTF e pentest profissional**, com foco obsessivo em **economia de
> tokens** (a IA opera dentro de uma janela de contexto finita).

Data: 2026-08-12 · Versão avaliada: v2.0 (7 tools MCP)

---

## 0. TL;DR — o que está quebrado hoje

O proxy captura tráfego para SQLite, mas **a IA não tem nenhuma tool MCP para ler,
buscar ou replayar esse tráfego**. As 5 tools "v1" anunciadas como entregues no
README (`list_requests`, `get_request_detail`, `replay_request`, `set_scope`,
`search_bodies`) **não existem no código** — só as 7 tools de gestão de
projeto/alvo estão registradas em `internal/mcpserver/server.go`.

Resultado: um bug bounty hunter conecta o browser, navega, gera 400 requests… e o
Claude só consegue dizer *"há 400 requests"*. Não consegue abrir nenhuma.

**Prioridade #1 absoluta: implementar e registrar as tools de leitura de dados.**

---

## 1. Bloco P0 — tornar o produto funcional (as tools v1 faltantes)

Sem isso, nada mais importa. O `store.Store` já expõe `List/Get/Count`; falta a
camada MCP e busca.

### 1.1 `list_requests` — token-frugal por design
Parâmetros: `method_filter?`, `status_filter?`, `host_filter?`, `path_contains?`,
`limit=50`, `offset=0`, `since_id?`.
Retorno **resumido** (nunca bodies): `id, ts, method, url, status, resp_len`.
- Padrão `limit=50` e **NUNCA** retornar body aqui — o hunter pagina, a IA escolhe
  o que abrir. Uma listagem de 50 linhas resumidas custa ~1-2k tokens; 50 bodies
  completos custam 50-500k. Essa é a decisão de design mais importante do projeto.

### 1.2 `get_request_detail` — com orçamento de tokens embutido
Parâmetros: `id`, `include=headers|body|all` (default `headers`),
`max_body_bytes=8192`, `body_range?` (ex: `0-4096`).
- Truncar body por padrão e sinalizar `"body_truncated": true, "total_len": N`.
- A IA pede o range que precisa. Nunca despejar um JS de 2MB no contexto.

### 1.3 `search_bodies` — o multiplicador de força
Parâmetros: `pattern` (regex/substring), `scope=req|resp|both`, `limit=30`.
Retorno: `id, url, match_snippet` (janela de ±80 chars ao redor do match), **não o
body inteiro**.
- É a tool mais valiosa para findings: "onde aparece `Authorization: Bearer`",
  "que resposta contém `SELECT * FROM`", "endpoints que refletem o parâmetro `q`".
- Implementar via SQL `LIKE` para substring + filtro Go para regex. Adicionar
  índice FTS5 (SQLite tem, o driver modernc suporta) numa v futura.

### 1.4 `replay_request` — o loop de exploração
Parâmetros: `id`, `header_overrides?`, `body_override?`, `method_override?`,
`url_override?`, `follow_redirects=false`.
- Reexecuta respeitando o scope guard (recusa replay fora de escopo — segurança).
- Retorno resumido + `new_request_id` (grava o replay no store para diff posterior).
- Sem isso não há IDOR testing, sem privilege escalation, sem fuzzing manual.

### 1.5 `set_scope`
Já existe a lógica em `scope.go`; falta a tool. Parâmetros: `in_scope[]`,
`out_of_scope[]`. Aplica no target ativo + persiste no `meta.yaml`.

> **Estimativa**: ~1 dia. Toda a plumbing (store, scope, audit) já existe.

> ✅ Entregue — ver roadmap v5.1 no README.

---

## 2. Bloco P1 — economia de tokens de nível profissional

Aqui está o diferencial que o usuário pediu. Um assistente de pentest bom não é o
que tem mais tools — é o que **cabe na janela de contexto** durante um engajamento
de 3 horas com 5.000 requests.

### 2.1 Deduplicação e agrupamento (`list_endpoints`)
Nova tool: agrupa requests por `(method, path-normalizado)` colapsando IDs/UUIDs/
hashes na URL (`/users/123` → `/users/{id}`). Retorna a **superfície de ataque**
em ~30 linhas em vez de 5.000.
- Um app real tem ~80 endpoints únicos e 5.000 hits. A IA quer os 80.
- Normalização: regex para segmentos numéricos, UUIDs, hex longo, base64.

> ✅ Entregue — ver roadmap v5.1 no README.

### 2.2 Diff de respostas (`diff_requests`)
Parâmetros: `id_a`, `id_b`, `mode=status|headers|body`.
Retorno: diff unificado compacto (só linhas alteradas).
- Essencial para IDOR ("resposta com meu token vs. token da vítima"), para detectar
  reflection, para auth bypass. Diff de 5 linhas em vez de 2 bodies inteiros.

> ✅ Entregue — ver roadmap v5.1 no README.

### 2.3 Resumo automático de resposta (`summarize_response`)
Para respostas grandes (HTML/JS), retornar estrutura extraída em vez do raw:
- HTML: forms (action/method/inputs), links, comentários, `<script src>`.
- JSON: schema de chaves + tipos (não os valores), profundidade limitada.
- JS: strings que parecem endpoints/URLs/tokens, chamadas `fetch`/`XHR`.
- Isso troca 200k tokens de body por 2k de sinal acionável.

> ✅ Entregue — ver roadmap v5.1 no README.

### 2.4 Content-type gating no proxy (economia na origem)
Hoje `onResponse` grava **todo** body in-scope. Adicionar política configurável:
- Não persistir body de `image/*`, `font/*`, `video/*`, `text/css` (só metadados).
- Cap de body por request (ex: 1MB) com flag `body_truncated`.
- Reduz o DB em 10-50x e evita que a IA sequer veja lixo binário.
- Ver `internal/proxy/proxy.go:203` (`if cap.inScope`).

> ✅ Entregue — ver roadmap v5.1 no README.

### 2.5 Perfil de resposta / anomaly hints
Coluna extra ou tool que marca requests "interessantes" heuristicamente:
status 5xx, respostas com `error/exception/stack trace`, tamanhos anômalos,
headers de debug (`X-Powered-By`, `Server`, `X-Debug`), `Set-Cookie` sem flags.
A IA pede `list_requests?interesting=true` e vai direto ao sinal.

> ✅ Entregue parcialmente — anomaly hints no `fuzz_request` + briefing de sessão no `get_active_context` (v5.1). Ver roadmap no README.

---

## 3. Bloco P2 — capacidades de pentest/CTF (as fases v3+ do PRD, repriorizadas)

O PRD já lista isso; reordeno por **valor/token para uma IA**:

| Prioridade | Capacidade | Tools MCP | Por quê agora |
|---|---|---|---|
| Alta | **Match & Replace** (v2.1) | `save_mr_rule`, `list_mr_rules`, `toggle_mr_rule` | Injeta headers/payloads sem o hunter tocar no browser. A IA controla. |
| Alta | **Scanner passivo** (v4) | `scan_passive_run`, `list_findings`, `finding_set_status` | Roda em cima do que já foi capturado — zero tráfego novo. Reflected XSS, secrets em JS, IDOR hints, headers ausentes. Findings estruturados = baratos em tokens. |
| Média | **Intruder/fuzzer** (v3) | `intruder_start/status/results/cancel` | Fuzzing com wordlist. Resultados devem voltar **agregados** (por status/tamanho), não request-a-request. |
| Média | **Sitemap** (v4) | `get_sitemap` | Árvore de endpoints navegável = mapa mental barato para a IA. |
| Baixa | **Decoder/comparer** (v5) | `decode`, `encode`, `compare` | Útil em CTF (base64/JWT/hex chains). A IA já faz muito disso nativamente. |
| Baixa | **Macros/sessão** (v3) | `macro_record/play` | Re-login automático para replays autenticados. Complexo; adiar. |

> ✅ Entregue — ver roadmap v5.1 no README.

### 3.1 CTF-specific (não está no PRD, mas o usuário pediu)
- **JWT tool**: `jwt_decode`/`jwt_resign` (alg=none, key confusion, weak secret). CTF
  web tem isso em 1 de cada 3 desafios.
- **`export_curl`**: dado um `id`, retorna o comando `curl` equivalente. O hunter
  cola no terminal; a IA raciocina sobre um one-liner em vez de um dump de headers.
- **`export_har`**: exporta o target inteiro como HAR — interop com outras tools.

> ✅ Entregue — ver roadmap v5.1 no README.

---

## 4. Bloco P3 — segurança e robustez (dívida técnica achada na avaliação)

Achados concretos no código atual:

1. **`Run(ctx)` ignora o context** (`server.go:135`) — já marcado com `ponytail:`.
   `ServeStdio` não cancela; num shutdown limpo o server pode vazar. Aceitável por
   ora, documentado.
2. **`InsecureSkipVerify` default true no upstream** (`proxy.go:117`) — correto para
   pentest (alvos com cert quebrado), mas **deve logar um aviso alto** no startup e
   idealmente ir para `config.yaml` por-target, não só flag de código.
3. **Sem limite de tamanho de body na captura** (`proxy.go:204`) — `io.ReadAll` sem
   cap. Um download grande in-scope estoura memória. Ver 2.4.
4. **`replay_request` precisa herdar o scope guard** — replay para host fora de
   escopo = tráfego não autorizado. Bloquear na tool, não confiar na IA.
5. **Redação do audit só cobre chaves conhecidas** (`audit.go`) — bodies capturados
   no SQLite **não são redigidos**. Documentar que o `requests.db` contém segredos
   em claro e merece `chmod 600` + aviso (é esperado numa proxy, mas deve ser
   explícito no threat model).
6. **CA RSA 2048** — ok, mas considerar ECDSA P-256 (handshake mais rápido, menos
   CPU sob carga de MITM). Baixa prioridade.
7. **Sem rotação do audit.log** — o próprio PRD lista como risco (§9). Rotacionar
   por tamanho.

---

## 5. Bloco P4 — ergonomia do assistente IA

Coisas que fazem a IA usar as tools *bem* (não só existir):

- **Descrições de tool em inglês e ricas**: hoje estão em português e curtas
  (`server.go:41`). O LLM decide qual tool chamar pela description. Descrições
  detalhadas com exemplos de quando usar reduzem tool-calls errados (= tokens
  desperdiçados). Incluir dicas de paginação/truncamento na própria description.
- **Retornos sempre estruturados e estáveis** (JSON com schema fixo) — a IA parseia
  melhor e você pode versionar.
- **Um `get_active_context` mais rico**: incluir contagem por status, top 5 hosts,
  se há scope definido. Um "briefing" de abertura de sessão em <500 tokens.
- **Prompt/README para o operador**: um bloco "como pedir para a IA" com exemplos
  ("liste endpoints únicos", "diff request 40 e 41", "procure Authorization nos
  bodies") — encurta a curva e evita a IA despejar tudo no contexto.
- **`system prompt` sugerido no MCP config**: instruir a IA a sempre paginar,
  sempre usar `search_bodies` antes de `get_request_detail`, nunca pedir body full
  sem range.

> ✅ Entregue parcialmente — descrições ricas em inglês, retornos estruturados,
> briefing (`get_active_context`) e guia do operador no README feitos; falta o
> system prompt sugerido no MCP config. Ver roadmap v5.1 no README.

---

## 6. Roadmap sugerido (reordenado por valor real)

```
SPRINT 1 (torna utilizável)     → Bloco P0 inteiro (§1). Sem isso o produto não faz o que promete.
SPRINT 2 (economia de tokens)   → §2.1 list_endpoints, §2.2 diff, §2.3 summarize, §2.4 body gating.
SPRINT 3 (achar bugs)           → §3 Match&Replace + Scanner passivo + findings.
SPRINT 4 (CTF + interop)        → §3.1 JWT/curl/HAR export, §2.5 anomaly hints.
CONTÍNUO                        → §4 segurança, §5 ergonomia (a cada tool nova).
```

**Nota sobre o README**: remover a alegação de que as tools v1 estão "✅ entregues"
até que estejam. Hoje é factualmente incorreto e induz o usuário a erro.

---

## 7. Uma frase por bloco (para priorizar rápido)

- **P0**: sem tools de leitura, a IA é cega. Faça isso primeiro. (~1 dia.)
- **P1**: dedup + diff + summarize é o que faz caber 5.000 requests numa janela de contexto.
- **P2**: scanner passivo dá o maior retorno em findings por token gasto.
- **P3**: cap de body e scope no replay são segurança, não features.
- **P4**: descrições de tool boas valem mais que tools novas.
