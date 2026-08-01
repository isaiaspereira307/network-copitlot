# mcp-proxy

Proxy MITM HTTP/HTTPS + servidor MCP para pentest e bug bounty assistido por IA.

## v2.0 — Workspaces

Organize capturas por projeto (engajamento) e alvo. Cada alvo tem storage SQLite
isolado em `~/.mcp-proxy/workspace/<projeto>/targets/<host>/requests.db`.

## Instalação

```bash
go install github.com/isaias/network-copitlot/cmd/mcp-proxy@latest
# ou, a partir do clone:
go build -o mcp-proxy ./cmd/mcp-proxy
```

O binário é autocontido. Dependências: Go 1.25+ para build; em runtime, apenas
acesso de escrita a `~/.mcp-proxy/`.

## Uso

### Modo MCP (stdio)

Configure seu cliente MCP (Claude Desktop, etc.) para executar `mcp-proxy`
sem argumentos. O servidor fala JSON-RPC via stdin/stdout.

Tools expostas:

| Tool | Função |
|---|---|
| `create_project` | Cria projeto (engajamento). Exige `name` e `type` (`bugbounty`\|`pentest`). |
| `list_projects` | Lista todos os projetos do workspace. |
| `set_active_project` | Define projeto ativo na config global. |
| `add_target` | Adiciona alvo ao projeto ativo. Exige `confirmed: true` (autorização). |
| `list_targets` | Lista alvos do projeto ativo. |
| `set_active_target` | Define alvo ativo; stores subsequentes abrem o `requests.db` deste alvo. |
| `get_active_context` | Retorna projeto + alvo ativos e contagem de requests capturados. |

### Modo CLI

```bash
mcp-proxy project create --name H1-EMPRESA --type bugbounty
mcp-proxy project list
mcp-proxy project use H1-EMPRESA

mcp-proxy target add --host api.empresa.com --confirm
mcp-proxy target list
mcp-proxy target use api.empresa.com
```

`target add` exige `--confirm` (declaração explícita de autorização). Sem a
flag, o comando aborta.

## Configuração

Estado persistido em `~/.mcp-proxy/config.yaml`:

```yaml
workspace_path: /home/user/.mcp-proxy/workspace
active_project: H1-EMPRESA
active_target:  api.empresa.com
```

Veja `config.yaml.example` para o template comentado. O arquivo é criado
automaticamente no primeiro `mcp-proxy` com defaults sensatos.

## Aviso legal

Use apenas em alvos com autorização explícita e por escrito. Esta ferramenta
captura e armazena tráfego de rede — operar contra sistemas sem permissão
configura crime na maioria das jurisdições. O `add_target` exige confirmação
explícita justamente para forçar essa decisão. Veja PRD §5.
