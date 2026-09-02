# mcp-proxy — Comandos & Uso

Referência completa: CLI (você digita no terminal) + tools MCP (Claude chama por você) + fluxos de bug bounty / pentest.

> **Autorização é pré-requisito.** `--confirm` (CLI) e `confirmed: true` (MCP) são obrigatórios ao adicionar um alvo. O scope guard bloqueia tráfego e replay fora do escopo. Só teste alvos com autorização escrita.

---

## 1. CLI

O binário tem dois modos:

- **Sem subcomando** → sobe o **servidor MCP** por stdio (é o que o Claude Desktop invoca).
- **Com subcomando** → gestão de projeto/alvo e o proxy MITM.

### Projetos (engajamentos)

```bash
mcp-proxy project create --name H1-EMPRESA --type bugbounty \
  --program "Empresa BBP" --platform hackerone   # program/platform opcionais
mcp-proxy project list
mcp-proxy project use H1-EMPRESA
```

`--type` = `bugbounty` | `pentest`.

### Alvos

```bash
mcp-proxy target add --host api.empresa.com --confirm \
  --in-scope "*.empresa.com,api.empresa.com" \
  --out-of-scope "blog.empresa.com" \
  --notes "prod api"
mcp-proxy target list
mcp-proxy target use api.empresa.com
```

`--in-scope` / `--out-of-scope` são CSV de padrões de host (glob `*.dominio`). `--confirm` é obrigatório.

### Proxy MITM

```bash
mcp-proxy proxy --addr 127.0.0.1:8080
# flags opcionais:
#   --body-cap 1048576                    cap de bytes por body (0 = default 1MB)
#   --no-body-content-types "image/*,font/*,video/*,text/css"  tipos a não capturar
```

Primeira execução gera a CA em `~/.mcp-proxy/ca/cert.pem` — instale como root CA no browser. Aponte o browser para `127.0.0.1:8080` (HTTP e HTTPS). Recarga de escopo é viva (mtime do `meta.yaml`), sem restart.

### Layout em disco

```
~/.mcp-proxy/
├── config.yaml            # projeto + alvo ativos
├── audit.log              # toda invocação de tool MCP (segredos redigidos)
├── ca/{cert,key}.pem      # CA MITM
└── workspace/<projeto>/targets/<host>/requests.db   # SQLite por-alvo
```

---

## 2. Configurar o MCP no Claude

```json
{
  "mcpServers": {
    "mcp-proxy": { "command": "mcp-proxy" }
  }
}
```

Reinicie o Claude Desktop. As 18 tools abaixo ficam disponíveis.

---

## 3. Tools MCP

Todas operam sobre o **projeto/alvo ativos**. Saídas são token-frugais: paginam, truncam e **nunca despejam bodies inteiros** fora do necessário.

### Gestão

| Tool | Args | Retorna |
|---|---|---|
| `create_project` | `name`, `type`(bugbounty\|pentest), `program?`, `platform?` | projeto criado |
| `list_projects` | — | lista de projetos |
| `set_active_project` | `name` | projeto ativo |
| `add_target` | `host`, `confirmed:true`, `in_scope?[]`, `out_of_scope?[]` | alvo criado |
| `list_targets` | — | alvos do projeto |
| `set_active_target` | `host` | alvo ativo |
| `set_scope` | `in_scope[]` (obrigatório; `[]` limpa) | escopo persistido (proxy recarrega vivo) |
| `get_active_context` | — | projeto, alvo, contagem de requests |

### Leitura de tráfego

| Tool | Args | Retorna |
|---|---|---|
| `list_requests` | `method_filter?`, `status_filter?`, `host_filter?`, `path_contains?`, `limit?`(50), `offset?`, `since_id?` | resumos (id, ts, method, url, status, resp_len) — sem bodies |
| `get_request_detail` | `id`, `include?`(headers\|body\|all), `max_body_bytes?`(8192), `body_range?`(ex `0-4096`) | request completo; bodies truncados com flag |
| `search_bodies` | `query` (regex se compilar, senão substring), `scope?`(req\|resp\|both), `limit?` | snippets ±80 char com id+url |
| `list_endpoints` | — | endpoints dedup (`/users/{id}`), hit_count, sample_ids |
| `summarize_response` | `id` | resumo por content-type: HTML (forms/links/scripts/comments), JSON (chaves+tipos, **sem valores**), JS (endpoints/calls/tokens) |
| `diff_requests` | `id_a`, `id_b`, `mode?`(resp\|req\|headers) | diff unificado compacto |

### Ação

| Tool | Args | Retorna |
|---|---|---|
| `replay_request` | `id`, `url?`, `method?`, `headers?{}`, `body?`, `follow_redirects?` | `new_request_id`, `status`, `resp_len` (novo request persistido) |
| `fuzz_request` | `id`, `point`, `payloads?[]`, `payload_set?`, `marker?`(FUZZ), `follow_redirects?` | tabela: payload, status, resp_len, reflected, new_id, anomaly — anomalias primeiro |
| `set_match_replace` | `rules[]` de `{part, match, replace, header?, name?, enabled?}` (`[]` limpa) | `rules_count` — regras persistidas; proxy aplica vivo |
| `list_match_replace` | — | regras persistidas do alvo |

**`fuzz_request` — pontos de injeção (`point`):**

| point | efeito |
|---|---|
| `marker` | substitui toda ocorrência de `marker` (default `FUZZ`) na URL + body + valores de header |
| `body` | substitui o body inteiro pelo payload |
| `url` | substitui a URL inteira pelo payload |
| `query:<param>` | seta o parâmetro de query `<param>` = payload (URL-encoded) |
| `header:<nome>` | seta o header `<nome>` = payload |

`payload_set` builtin: `xss`, `sqli`, `traversal`, `redirect`. Combina com `payloads[]`. Cap: 100 payloads/run. **anomaly** = status mudou vs baseline, tamanho variou >20%, ou o payload refletiu no corpo. Cada payload passa pelo scope guard; fora de escopo → erro só naquela linha, não aborta o run.

---

## 4. Fluxos de bug bounty / pentest

Prepare uma vez: `project create` → `project use` → `target add --confirm` → `target use` → instale a CA → aponte o browser → navegue o alvo autorizado. Depois converse com o Claude.

### Recon — mapa de ataque
> "Rode `list_endpoints`. Marque os que parecem admin/interno ou que recebem id."

### Secrets & info leak
> "`search_bodies query=\"eyJ[A-Za-z0-9_-]+\" scope=both`" — JWTs
> "`search_bodies query=\"AKIA[0-9A-Z]{16}\"`" — AWS keys
> "`summarize_response` em todos os `.js` e liste endpoints/tokens."

### IDOR / BOLA
> "Pega o GET `/api/orders/1001`. `fuzz_request point=query:id payloads=[1000,1001,...,1010]` e me diz quais 200 com tamanho diferente."
> Ou via marker: URL com `/orders/FUZZ`, `point=marker`.

### Broken auth / privilege escalation
> "`replay_request` sem o header `Authorization`. Depois com o cookie low-priv. `diff_requests mode=resp` das duas respostas — vazou dado sensível sem auth?"

### XSS (reflected)
> "`fuzz_request id=42 point=query:q payload_set=xss`. Liste as linhas `reflected=true` — payload voltou sem encode."

### SQLi (erro / boolean / time)
> "`fuzz_request id=42 point=query:id payload_set=sqli`. Anomalias por status 500 ou variação de tamanho = candidato a SQLi. Confirme com `get_request_detail` no `new_id`."

### Path traversal / LFI
> "`fuzz_request point=query:file payload_set=traversal`. `reflected=true` com conteúdo de `/etc/passwd` = LFI."

### Open redirect
> "`fuzz_request point=query:next payload_set=redirect follow_redirects=false`. Status 30x apontando pra `evil.example` no `Location` = open redirect."

### Header injection / trust bypass
> "`fuzz_request point=header:X-Forwarded-For payloads=[\"127.0.0.1\",\"localhost\"]` num endpoint admin — mudou o status?"

### Regressão / verificação de patch
> "`diff_requests` do request antigo vs o replay novo — o fix mudou a resposta?"

---

## 5. Match/replace vivo

`set_match_replace` reescreve **tráfego vivo no proxy**: regras aplicadas a cada request in-scope antes de encaminhar ao upstream. Persistidas no `meta.yaml` do alvo; o proxy recarrega via mtime — **sem restart**.

Cada regra:

| campo | valor |
|---|---|
| `part` | `url` \| `req_header` \| `req_body` |
| `match` | regex (RE2) |
| `replace` | string de substituição (`$1`, `${nome}`) |
| `header` | nome do header (obrigatório se `part=req_header`) |
| `name` | rótulo opcional |
| `enabled` | default `true` |

**Segurança:** uma regra `url` cujo rewrite jogaria o host para fora do escopo é **descartada em runtime** — match/replace nunca vaza tráfego pro alvo errado. Regras são validadas (part válido, regex compila) ao salvar.

Exemplos:
> "`set_match_replace` com uma regra: `part=req_header header=X-Forwarded-For match=.* replace=127.0.0.1` — testa bypass de restrição por IP em todo o tráfego."
> "Reescreve `\"role\":\"user\"` → `\"role\":\"admin\"` em req_body pra ver se o backend confia no cliente."
> "`part=url match=/v1/ replace=/v2/` — força toda navegação pra API v2."
> "`list_match_replace`" — vê as regras ativas. `set_match_replace rules=[]` limpa tudo.

Match/replace **pontual** (um request só) continua via overrides do `replay_request` e via `fuzz_request point=marker`.

---

## 6. Analítica

```bash
mcp-proxy proxy --addr 127.0.0.1:8080   # logs de escuta no startup
cat ~/.mcp-proxy/audit.log              # trilha de auditoria (segredos redigidos)
```
