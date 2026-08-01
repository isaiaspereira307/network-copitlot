# PRD — MCP Proxy (Golang)

## 1. Visão Geral

**Nome do projeto:** `mcp-proxy` (nome provisório)

**Resumo:** Ferramenta em Go que combina um proxy de interceptação HTTP/HTTPS (MITM) com um servidor MCP, expondo o ciclo completo de pentest/bug bounty — captura, organização, manipulação, fuzzing, detecção e reporting — como *tools* consumíveis por um assistente de IA (Claude). O objetivo é auxiliar análises de segurança em programas de bug bounty e pentests autorizados.

**Diferencial:** primeira suíte de pentest **nativa-MCP**. Caido/Burp são GUI-first; aqui a IA é o operador primário, o humano acessa via CLI.

**Fora de escopo permanente:** Web UI, multiusuário, SaaS, scanner de produção sem opt-in explícito, qualquer uso não autorizado.

**Fora de escopo nesta primeira versão (v1):** v1 entrega apenas o core (ver §4.1). Demais funcionalidades estão no roadmap em fases (v2+, §4.2–§4.8).

---

## 2. Objetivos da v1 (MVP)

- Interceptar tráfego HTTP/HTTPS de um navegador ou app Android configurado para usar o proxy.
- Armazenar as transações capturadas (request + response) em memória (ou SQLite simples).
- Expor essas transações via MCP para consulta pelo Claude.
- Permitir reenvio manual de uma requisição capturada, com modificações (equivalente básico ao "Repeater").
- Restringir a interceptação a um escopo de domínios definido pelo usuário (proteção contra captura fora do alvo autorizado).

**Não-objetivo:** automatizar ataques ou varreduras sem intervenção humana. A ferramenta é um *facilitador de análise*, não um scanner autônomo.

---

## 3. Personas

- **Pesquisador de bug bounty**: quer entender rapidamente a superfície de um app/site alvo, achar padrões de parâmetros, tokens expostos, endpoints não documentados, validar findings.
- **Pentester**: quer acelerar a fase de reconhecimento e triagem de um engajamento com escopo já definido pelo cliente.

---

## 4. Escopo funcional

### 4.1 v1.0 — Core Proxy + MCP (entregue)

#### 4.1.1 Proxy MITM

| Item | Descrição |
|---|---|
| Interceptação HTTP/HTTPS | Proxy local que intercepta tráfego do navegador/dispositivo configurado manualmente. |
| CA própria | Geração de certificado raiz para MITM em HTTPS, instalável no navegador/emulador Android. |
| Escopo (scope) | Lista de domínios permitidos; tráfego fora do escopo é apenas repassado (passthrough), sem log completo de corpo. |
| Armazenamento | Cada transação (request + response) salva com: método, URL, headers, body, status, timestamp. Em v1 usa store único (memória ou `requests.db` flat); em v2+ migra para `workspace/<project>/<target>/requests.db` (ver §4.2). |

#### 4.1.2 Servidor MCP — Tools da v1

| Tool | Parâmetros | Retorno |
|---|---|---|
| `list_requests` | `domain_filter` (opcional), `method_filter` (opcional), `limit` (opcional) | Lista resumida de transações (id, método, URL, status, timestamp) |
| `get_request_detail` | `id` | Detalhe completo de uma transação (headers, body de request e response) |
| `replay_request` | `id`, `header_overrides` (opcional), `body_override` (opcional), `method_override` (opcional) | Resultado da nova requisição (status, headers, body) |
| `set_scope` | `domains []string` | Confirmação do escopo ativo |
| `search_bodies` | `pattern` (regex ou substring) | Lista de transações cujo body (request ou response) contém o padrão |

#### 4.1.3 Configuração
- Arquivo de config (`config.yaml` ou flags de CLI) definindo: porta do proxy, escopo inicial, caminho do CA, modo de armazenamento (memória vs SQLite).

---

### 4.2 v2.0 — Workspaces (Projects + Targets)

**Objetivo:** organizar capturas por engajamento (programa de bug bounty / contrato de pentest), segregando dados por alvo.

**Modelo de dados:**

- **Projeto** = engajamento (ex.: `HackerOne-EMPRESA`, `Pentest-ClienteY`).
  - Tem: nome, tipo (bugbounty/pentest), programa, plataforma, criado_em.
  - Pode ter **N alvos**.
- **Alvo** = host/domínio dentro do projeto (ex.: `api.empresa.com`, `app.empresa.com`).
  - Tem: in_scope, out_of_scope_patterns, notes.
  - Tem storage isolado (`requests.db` SQLite, `findings.db`).

**Storage segregado:**

```
~/.mcp-proxy/workspace/
└── <project>/
    ├── meta.yaml
    ├── config.yaml          # scope global do projeto, M&R rules (futuro)
    └── targets/
        └── <host>/
            ├── meta.yaml
            ├── requests.db
            ├── findings.db   # preenchido em v4+
            ├── repeater/     # v2.1
            ├── intruder/     # v3
            ├── macros/       # v3
            ├── sitemap.json  # v4
            └── extensions/   # v5
```

**Config persistida:** `~/.mcp-proxy/config.yaml` com workspace path, projeto ativo, alvo ativo.

**MCP tools novas (v2):**

| Tool | Parâmetros | Retorno |
|---|---|---|
| `create_project` | `name`, `type` (bugbounty/pentest), `program`, `platform` | Confirmação + id do projeto |
| `list_projects` | — | Lista de projetos |
| `set_active_project` | `name` | Confirmação |
| `get_active_context` | — | Projeto ativo + alvo ativo + contagem de requests |
| `add_target` | `host`, `in_scope_patterns`, `out_of_scope_patterns` | Confirmação + id do alvo |
| `list_targets` | `project` (opcional, default ativo) | Lista de alvos do projeto |
| `set_active_target` | `host` | Confirmação |

**CLI:** `mcp-proxy project create|list|use`, `mcp-proxy target add|list|use`.

**Sucesso:** criar projeto, adicionar 2 alvos, requests via proxy aparecem isoladas, IA consegue alternar contexto entre alvos.

---

### 4.3 v2.1 — Manipulação Avançada

**Objetivo:** Repeater++ (editor de request cru) + Match & Replace on-the-fly.

**Custom Requests (Repeater++):**
- Editor de request cru (método, URL, headers, body).
- Multi-tab.
- Histórico persistido por alvo em `repeater/tabs.json`.

**Match & Replace:**
- Regras on-the-fly em headers / params / body.
- Escopo: projeto (global) ou alvo (específico).
- Suporte a regex e expressões simples.

**MCP tools novas (v2.1):**

| Tool | Parâmetros | Retorno |
|---|---|---|
| `send_custom_request` | `method`, `url`, `headers`, `body` | Resposta completa |
| `list_repeater_tabs` | `target` (opcional) | Lista de tabs salvas |
| `save_match_replace_rule` | `scope`, `where` (header/param/body), `match`, `replace`, `regex` (bool) | Confirmação |
| `list_mr_rules` | `scope` | Lista de regras M&R |
| `toggle_mr_rule` | `id`, `enabled` | Confirmação |

**Sucesso:** editar e reenviar request arbitrária; regra M&R substitui header em todas as requests em flight.

---

### 4.4 v3.0 — Fuzzing

**Objetivo:** Intruder (fuzzer) + Macro/session handling.

**Intruder:**
- 4 attack types: Sniper, Battering Ram, Pitchfork, Cluster Bomb.
- Payload lists: arquivo externo + built-in (`examples/payloads/`).
- Grep match / extract (regex em response).
- Throttle (req/s) configurável.
- Resultados persistidos em `intruder/jobs/<id>/results.json`.

**Macro / Session handling:**
- Cadeia de requests para manter sessão (login + csrf + replay).
- Variáveis extraídas via regex durante record.
- Replay automático antes de cada job intruder que dependa da sessão.

**MCP tools novas (v3):**

| Tool | Parâmetros | Retorno |
|---|---|---|
| `intruder_start` | `base_request_id`, `attack_type`, `payload_sets`, `throttle_rps` | Job id |
| `intruder_status` | `job_id` | Status (queued/running/done) + progresso |
| `intruder_results` | `job_id`, `grep` (opcional) | Lista de resultados |
| `intruder_cancel` | `job_id` | Confirmação |
| `macro_record` | `name` | Session id (inicia captura) |
| `macro_play` | `session_id` | Confirmação (replay) |
| `macro_list` | — | Lista de macros salvas |

**Sucesso:** rodar cluster-bomb em 2 params com 50 payloads cada, identificar respostas divergentes; macro mantém sessão por 10 minutos.

---

### 4.5 v4.0 — Detecção Passiva

**Objetivo:** scanner passivo (sem envio de payloads) + sitemap passivo.

**Scanner passivo — detectores:**

| Tipo | Heurística |
|---|---|
| XSS refletido | param aparece verbatim no body de response |
| IDOR | request com `id` numérico/UUID em path/param; comparar responses de IDs próximos (deltas de tamanho/conteúdo) |
| SQLi | error patterns em response body (`sql syntax`, `mysql_`, `ORA-`, `pg_query`, etc) |
| SSRF | param aceita URL + response contém dados de rede interna (IPs privados, metadata) |
| Open redirect | param com URL + 3xx com `Location` externo ao escopo |
| Secrets em JS | regex para AWS keys, GitHub tokens, JWT, Google API keys, etc |

**Sitemap passivo:** árvore de endpoints extraída do tráfego capturado, persistida em `sitemap.json`.

**MCP tools novas (v4):**

| Tool | Parâmetros | Retorno |
|---|---|---|
| `scan_passive_run` | `target` (opcional) | Job id |
| `scan_passive_status` | `job_id` | Status + count por tipo |
| `list_findings` | `target`, `type` (opcional), `severity` (opcional) | Lista priorizada |
| `get_finding_detail` | `finding_id` | Evidência + request relacionado |
| `finding_set_status` | `finding_id`, `status` | Confirmação |
| `get_sitemap` | `target` | Árvore de endpoints |

**Sucesso:** IA recebe lista priorizada de findings com evidência após navegação no alvo.

---

### 4.6 v4.1 — Detecção Ativa

**Objetivo:** scanner ativo (envia payloads próprios) + crawler leve, ambos **opt-in duplo**.

**Scanner ativo:**
- Envia payloads seguros (XSS, SQLi, SSRF, redirect).
- Throttle agressivo (default 1 req/s, ajustável).
- Ban list de payloads destrutivos (`DROP TABLE`, `rm -rf`).
- Requer **flag `--i-know-what-im-doing`** na inicialização **e** confirmação interativa ao rodar.

**Crawler:**
- Depth limitado (default 3).
- Respeita `robots.txt`.
- Alimenta sitemap ativo.
- Mesma política de opt-in.

**MCP tools novas (v4.1):**

| Tool | Parâmetros | Retorno |
|---|---|---|
| `scan_active_start` | `target`, `types`, `throttle_rps` | Job id |
| `scan_active_status` | `job_id` | Status + count |
| `scan_active_cancel` | `job_id` | Confirmação |
| `crawler_start` | `target`, `max_depth` | Job id |
| `crawler_status` | `job_id` | Status + URLs descobertas |
| `crawler_cancel` | `job_id` | Confirmação |

**Sucesso:** scan ativo identifica XSS armazenado em endpoint autorizado, finding registrado.

---

### 4.7 v5.0 — Extensibilidade

**Objetivo:** Decoder + Comparer + Logger++ + Extensions API (plugins Go).

**Decoder:** encode/decode multi-formato (base64, URL, hex, JWT, HTML entities, gzip inflate/deflate, char encoding).

**Comparer:** diff visual lado a lado de 2 requests/responses.

**Logger++:** filtros avançados (regex em qualquer campo), coloração, tags custom em requests, comentários.

**Extensions API (BApps-like):**
- Plugins Go compilados (carregados via stdlib `plugin` package).
- Limitação conhecida: `plugin` package é Linux/macOS only; em Windows plugins precisam ser linkados como build-tag ou via mecanismo alternativo (a definir em `docs/extensions-api.md`).
- Hooks: `on_request`, `on_response`, `on_finding`.
- Whitelist de plugins permitidos por projeto.
- Documentação separada em `docs/extensions-api.md`.

**MCP tools novas (v5):**

| Tool | Parâmetros | Retorno |
|---|---|---|
| `decode` | `format`, `input` | Output decodificado |
| `encode` | `format`, `input` | Output encodado |
| `compare` | `left_id`, `right_id`, `kind` (request/response) | Diff |
| `tag_request` | `request_id`, `tag` | Confirmação |
| `add_comment` | `request_id`, `comment` | Confirmação |
| `list_tags` | `target` | Lista de tags em uso |
| `ext_list` | `target` | Lista de extensions carregadas |
| `ext_enable` | `target`, `ext_name` | Confirmação |
| `ext_disable` | `target`, `ext_name` | Confirmação |

**Sucesso:** decoder converte base64→texto em uma call; extension custom detecta AWS key e cria finding.

---

### 4.8 v5.1 — Reporting

**Objetivo:** export de findings em Markdown/HTML/PDF + findings tracking.

**Findings tracking:** status `open | triaged | confirmed | false-positive | closed`.

**Templates:** HackerOne-style (título, descrição, steps, impact, fix).

**Export:** Markdown (puro-Go via `goldmark`), HTML (template), PDF (via `chromedp` headless).

**MCP tools novas (v5.1):**

| Tool | Parâmetros | Retorno |
|---|---|---|
| `report_export_markdown` | `target`, `status_filter` | Path do arquivo |
| `report_export_html` | `target`, `status_filter` | Path do arquivo |
| `report_export_pdf` | `target`, `status_filter` | Path do arquivo |

**Sucesso:** exportar relatório HackerOne-ready de 10 findings em <5s.

---

## 5. Requisitos não-funcionais

- **Linguagem/stack:** Go (latest stable), usando:
  - `elazarl/goproxy` (proxy MITM)
  - `mark3labs/mcp-go` ou SDK oficial `go-sdk` (servidor MCP)
  - `modernc.org/sqlite` (driver puro Go, sem CGO)
  - `spf13/cobra` (CLI, v2+)
  - `gocolly/colly` (crawler, v4.1)
  - `jung-kurt/govaluate` (eval expressões M&R, v2.1)
  - `yuin/goldmark` (Markdown report, v5.1)
  - `chromedp/chromedp` opcional (PDF headless, v5.1)
- **Transporte MCP:** stdio (v1) → HTTP+SSE (v2.1+).
- **Desempenho:**
  - v1: 100 req/s sustentado.
  - v3: intruder com 10 workers paralelos.
  - v4: scanner passivo com p99 < 200ms sobre captura.
- **Storage:** SQLite WAL, 1 writer por arquivo, índices em `(target_id, ts)` e `(target_id, method, url)`.
- **Concorrência:** goproxy goroutines; scanner worker pool com channel; intruder rate-limited cancelable.
- **Segurança:**
  - CA gerado localmente, nunca commitado no repositório.
  - Escopo obrigatório por target antes de interceptar corpo.
  - Opt-in duplo para active scanner e crawler (flag CLI + confirmação interativa).
  - Confirmação ao adicionar target: "Tem autorização para testar este alvo?" (default: aborta).
  - Audit log: cada tool MCP registra `tool/user/ts/action` em `~/.mcp-proxy/audit.log`.
  - Plugins em allowlist por projeto (v5).
  - Sem envio externo de dados capturados.
- **Portabilidade:** binário único, sem dependências externas em runtime, sem CGO.
- **Privacidade:** opção de criptografar SQLite com SQLCipher (puro-Go) — feature opcional, não default.

---

## 6. Fora de escopo (explicitamente adiado)

**Mantido do v1 (continua fora em qualquer versão):**
- Suporte a certificate pinning bypass (Frida/objection) — pré-requisito externo do usuário.
- Interface web/dashboard.
- Múltiplos usuários/sessões simultâneas.
- Persistência distribuída.
- Integração com Burp/ZAP como alternativa ao proxy nativo.

**Movido para fases (não é mais "fora de escopo permanente"):**
- Scanner passivo/ativo: entregue em v4.0 / v4.1.

**Adicionado em versões futuras (além de v5.1):**
- Mobile app (Android/iOS).
- Scan de binários (DEX/APK).
- Plugins em outras linguagens (WASM, Lua) — apenas Go nativo em v5.
- Scanner baseado em IA (LLM-as-judge sobre findings) — pode ser via tools MCP, mas não automatizado.
- Integração com Nuclei templates.

---

## 7. Critérios de sucesso por fase

| Versão | Critério de aceite |
|---|---|
| v1.0 | Navegador configurado no proxy navega em site de teste; Claude lista e detalha requests via MCP; replay com modificações funciona; out-of-scope sem corpo armazenado. |
| v2.0 | Criar projeto, adicionar 2 alvos, requests via proxy aparecem isoladas por alvo, IA alterna contexto entre alvos. |
| v2.1 | Editor de request cru; regra M&R substitui header em flight. |
| v3.0 | Cluster-bomb em 2 params × 50 payloads identifica divergências; macro mantém sessão por 10min. |
| v4.0 | IA recebe lista priorizada de findings com evidência após navegação. |
| v4.1 | Active scan identifica XSS armazenado em alvo autorizado; finding registrado. |
| v5.0 | Decoder converte base64→texto em 1 call; extension custom detecta AWS key e cria finding. |
| v5.1 | Export de 10 findings em <5s em MD/HTML/PDF. |

---

## 8. Estrutura de projeto sugerida

```
mcp-proxy/
├── cmd/
│   └── mcp-proxy/main.go
├── internal/
│   ├── proxy/                # goproxy setup, CA, hooks
│   ├── store/                # SQLite por alvo, queries, indices
│   ├── projects/             # Project/Target model + repo
│   ├── repeater/             # custom request + tabs
│   ├── intruder/             # fuzzing engine
│   ├── scanner/              # passive + active detectors
│   ├── macro/                # session handling
│   ├── sitemap/              # passivo + ativo
│   ├── decoder/              # encode/decode
│   ├── comparer/             # diff
│   ├── logger/               # filtros, tags, comments
│   ├── extensions/           # plugin loader
│   ├── report/               # export MD/HTML/PDF
│   ├── mcpserver/            # agregador de tools MCP
│   └── config/               # load config, paths, active state
├── docs/
│   ├── architecture.md
│   ├── threat-model.md
│   └── extensions-api.md     # v5
├── examples/
│   ├── extensions/           # plugins de exemplo
│   └── payloads/             # listas built-in
├── test/
│   ├── integration/          # httptest
│   └── e2e/                  # fluxo MCP completo
├── config.yaml.example
├── go.mod
├── go.sum
├── README.md
└── LICENSE
```

---

## 9. Riscos e mitigações

| Risco | Mitigação |
|---|---|
| Uso indevido fora de escopo autorizado | Confirmação por target + audit log + warning no README + opt-in para active |
| Certificate pinning em apps Android | Documentar como limitação conhecida, ferramentas externas (Frida) |
| Vazamento de dados sensíveis (tokens, senhas) em logs/arquivos | Storage local only; opção de DB criptografado; sem envio externo |
| Active scanner abusivo | Rate limit hard, opt-in duplo, ban list de payloads destrutivos |
| Plugin malicioso (extensions v5) | Allowlist por projeto, documentação clara da superfície de hooks, warning |
| Crescimento do código dificulta manutenção | Boundaries claras entre módulos, testes por módulo, evitar god-objects |
| SQLite contention com intruder pesado | WAL mode + 1 writer por target + batch inserts |
| Dependência de libs Go abandonadas | Pin versões, avaliar forks; preferir libs com stars>500 e commit recente |
| Caido/Burp lançam versão MCP antes | Diferencial: profundidade de integração IA + extensibilidade via plugins Go |
| Performance degrada com muito tráfego | Sampler opcional, M&R só aplica se regra casa, índices em SQLite |
| Conflito de concorrência em scanner passivo | Fila channel + worker pool + write assíncrono |
| Audit log crescer indefinidamente | Rotação por tamanho, retenção configurável |

---

## 10. Anexo A — Lista consolidada de MCP tools por fase

| Fase | Tools |
|---|---|
| v1 | `list_requests`, `get_request_detail`, `replay_request`, `set_scope`, `search_bodies` |
| v2 | + `create_project`, `list_projects`, `set_active_project`, `get_active_context`, `add_target`, `list_targets`, `set_active_target` |
| v2.1 | + `send_custom_request`, `list_repeater_tabs`, `save_match_replace_rule`, `list_mr_rules`, `toggle_mr_rule` |
| v3 | + `intruder_start`, `intruder_status`, `intruder_results`, `intruder_cancel`, `macro_record`, `macro_play`, `macro_list` |
| v4 | + `scan_passive_run`, `scan_passive_status`, `list_findings`, `get_finding_detail`, `finding_set_status`, `get_sitemap` |
| v4.1 | + `scan_active_start`, `scan_active_status`, `scan_active_cancel`, `crawler_start`, `crawler_status`, `crawler_cancel` |
| v5 | + `decode`, `encode`, `compare`, `tag_request`, `add_comment`, `list_tags`, `ext_list`, `ext_enable`, `ext_disable` |
| v5.1 | + `report_export_markdown`, `report_export_html`, `report_export_pdf` |

**Total: 33 tools** ao final do roadmap.

---

## 11. Anexo B — Schema do banco SQLite

### Tabela `requests` (por alvo)

```sql
CREATE TABLE requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,                  -- unix epoch ms
  method TEXT NOT NULL,
  url TEXT NOT NULL,
  req_headers TEXT NOT NULL,            -- JSON
  req_body BLOB,
  status INTEGER,
  resp_headers TEXT,                    -- JSON
  resp_body BLOB,
  resp_len INTEGER,
  ttfb_ms INTEGER,
  tags TEXT,                            -- JSON array
  notes TEXT
);
CREATE INDEX idx_requests_ts ON requests(ts);
CREATE INDEX idx_requests_method_url ON requests(method, url);
```

### Tabela `findings` (por alvo)

```sql
CREATE TABLE findings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  type TEXT NOT NULL,                   -- XSS | IDOR | SQLi | SSRF | redirect | secret | other
  severity TEXT NOT NULL,               -- info | low | med | high | crit
  evidence TEXT NOT NULL,               -- JSON: {request_id, snippet, payload, ...}
  status TEXT NOT NULL DEFAULT 'open',  -- open | triaged | confirmed | false-positive | closed
  notes TEXT
);
CREATE INDEX idx_findings_ts ON findings(ts);
CREATE INDEX idx_findings_status_severity ON findings(status, severity);
```

### `meta.yaml` (projeto)

```yaml
name: HackerOne-EMPRESA
type: bugbounty          # bugbounty | pentest
program: EMPRESA
platform: hackerone
created_at: 2026-08-01T12:00:00Z
```

### `meta.yaml` (alvo)

```yaml
host: api.empresa.com
in_scope:
  - "*.empresa.com"
  - "api.empresa.com"
out_of_scope_patterns:
  - "*.admin.empresa.com"
notes: "API v2, auth via Bearer token"
```

---

## 12. Roadmap de releases

| Versão | Conteúdo | Esforço relativo |
|---|---|---|
| v1.0 | (entregue) Core | feito |
| v2.0 | Projects + Targets + storage segregado | pequeno |
| v2.1 | Repeater++ + Match & Replace | médio |
| v3.0 | Intruder + Macro | grande |
| v4.0 | Scanner passivo + Sitemap passivo | grande |
| v4.1 | Scanner ativo + Crawler | médio |
| v5.0 | Decoder + Comparer + Logger++ + Extensions API | grande |
| v5.1 | Reporting + Findings tracking | médio |

"Esforço relativo" é magnitude de implementação (pequeno/médio/grande), não prazo.
