# mcp-proxy

> MITM HTTP/HTTPS proxy + MCP server for AI-assisted bug bounty and pentesting.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-compatible-purple)](https://modelcontextprotocol.io)
[![Platform: Linux/macOS/Windows](https://img.shields.io/badge/platform-linux%20%7C%20macOS%20%7C%20windows-lightgrey)]()

A native-MCP interception proxy. Point your browser at it, browse your authorized target, and an AI assistant (Claude) reads, searches, replays, fuzzes, and rewrites captured transactions over MCP — 18 tools, no GUI.

```
  browser ─── HTTP/HTTPS ──▶  mcp-proxy  ──── upstream
            MITM CA installed      │
                                  ├── SQLite (per-target)
                                  └── MCP tools ◀──── Claude
```

**Why this exists.** Burp and Caido are GUI-first. This project flips the model: the AI is the primary operator, the human drives the browser, and every captured request is a structured tool the LLM can reason over.

---

## ✨ What you get

- **Real MITM** for HTTP and HTTPS (auto-generated CA, one-time install).
- **Per-target storage** — separate SQLite for every host you add.
- **MCP tools** to manage projects/targets, list/inspect/search/replay transactions, map endpoints, diff & summarize responses, fuzz (intruder-lite), and live match/replace.
- **Hard scope guard** — out-of-scope traffic is logged without bodies.
- **Zero CGO, zero runtime deps** — single static binary.
- **Audit log** with automatic redaction of secrets (`password`, `token`, `api_key`, etc).

---

## 📦 Installation

### go install (requires Go 1.25+)

```bash
go install github.com/isaiaspereira307/network-copitlot/cmd/mcp-proxy@latest
```

Installs the `mcp-proxy` binary into `$(go env GOPATH)/bin`. Add that directory to your `PATH` if it isn't already.

Pre-compiled binaries for Linux (amd64/arm64) and Windows (amd64) are attached to every [GitHub Release](https://github.com/isaiaspereira307/network-copitlot/releases) — the workflow builds them automatically when a `v*` tag is pushed:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

### From source (requires Go 1.25+)

```bash
git clone https://github.com/isaiaspereira307/network-copitlot
cd network-copitlot
go build -o mcp-proxy ./cmd/mcp-proxy
sudo mv mcp-proxy /usr/local/bin/   # or anywhere in PATH
```

### Verify

```bash
mcp-proxy --help  # shows project, target, proxy subcommands
```

---

## 🚀 Tutorial (5 minutes to first capture)

This walks you through your first intercepted transaction, from a clean install to a Claude-readable result.

### 1. Create a project and target

```bash
mcp-proxy project create --name H1-EMPRESA --type bugbounty
mcp-proxy project use H1-EMPRESA
mcp-proxy target add --host api.empresa.com --confirm
mcp-proxy target use api.empresa.com
```

> **The `--confirm` flag is mandatory.** It forces you to acknowledge that you have written authorization to test the target. Skipping it aborts the command. This is a hard guard, not a checkbox.

### 2. Generate and install the CA

```bash
# start the proxy once to generate ~/.mcp-proxy/ca/cert.pem
mcp-proxy proxy --addr 127.0.0.1:8080
# in another terminal:
ls ~/.mcp-proxy/ca/
# cert.pem  key.pem
```

Install `cert.pem` as a trusted root CA:

| Browser     | Path                                                        |
|-------------|-------------------------------------------------------------|
| Firefox     | Settings → Privacy & Security → Certificates → View → Import |
| Chrome/Edge | Settings → Privacy → Security → Manage certificates → Import |
| Burp/Caido  | import via their own UI; this CA is a standard X.509         |

For mobile (Android emulator): push the cert and add via `Settings → Security → Install from storage`.

### 3. Configure your browser to use the proxy

| Setting      | Value            |
|--------------|------------------|
| HTTP proxy   | `127.0.0.1:8080` |
| HTTPS proxy  | `127.0.0.1:8080` |
| SOCKS        | leave blank      |

Restart the proxy if you change `--addr`. The proxy logs the listening address on startup.

### 4. Browse your target

Hit any endpoint on `*.empresa.com`. Every request + response lands in:

```
~/.mcp-proxy/workspace/H1-EMPRESA/targets/api.empresa.com/requests.db
```

### 5. Read it from Claude

Add `mcp-proxy` to your MCP client config (Claude Desktop example):

```json
{
  "mcpServers": {
    "mcp-proxy": {
      "command": "mcp-proxy"
    }
  }
}
```

Restart Claude Desktop. You now have access to these tools:

| Tool                  | What it does                                            |
|-----------------------|---------------------------------------------------------|
| `create_project` / `list_projects` / `set_active_project` | Manage engagements          |
| `add_target` / `list_targets` / `set_active_target`       | Manage targets (`confirmed: true` required) |
| `set_scope`           | Persist in-scope patterns (proxy reloads live)          |
| `get_active_context`  | Session briefing: status histogram, top hosts, endpoints, scope |
| `list_requests`       | Paginate captured requests (summaries, no bodies)       |
| `get_request_detail`  | Full request/response (bodies truncated + paged)        |
| `search_bodies`       | Regex/substring search across req/resp bodies           |
| `list_endpoints`      | Deduplicated endpoint map (`/users/{id}`)               |
| `summarize_response`  | Per-content-type summary (HTML forms, JSON keys, JS endpoints/tokens) |
| `diff_requests`       | Compact unified diff of two requests                    |
| `replay_request`      | Re-send with url/method/header/body overrides           |
| `fuzz_request`        | Intruder-lite: inject payloads at a point, flag anomalies |
| `set_match_replace` / `list_match_replace` | Live in-flight request rewriting   |
| `intruder_start` / `intruder_status` / `intruder_results` / `intruder_cancel` | Full Intruder (sniper, battering ram, pitchfork, cluster bomb) |
| `macro_record` / `macro_play` / `macro_list` | Session handling: macro chains + variable extraction |
| `scan_passive_run` / `scan_passive_status` | Passive scanner (XSS, SQLi, SSRF, redirect, secrets, IDOR hints) |
| `list_findings` / `get_finding_detail` / `finding_set_status` | Findings lifecycle (open→closed) |
| `get_sitemap` | Passive endpoint tree |
| `scan_active_start` / `scan_active_status` | Active scanner (double opt-in: `MCP_PROXY_ACTIVE=1` + `confirmed=true`) |
| `crawl_start` | Active crawler discovery |
| `export_curl` | Rebuild a captured request as a ready-to-paste curl command |
| `export_har` | Export the active target as HAR 1.2 (metadata only) |
| `jwt_decode` | Decode a JWT with attack-surface warnings (alg=none, empty sig, expired) |
| `jwt_resign` | Re-sign a JWT offline (none/HS256) — requires `confirmed: true` |

Then in Claude:

> *"What's the current project and target?"*

Claude calls `get_active_context`. From there you can list, search, diff, replay, and fuzz captured traffic entirely from the chat — see [docs/COMMANDS.md](docs/COMMANDS.md) for every tool and worked bug-bounty flows.

---

## 🧠 How it works

```
┌──────────┐     HTTPS      ┌──────────────┐     HTTPS      ┌──────────┐
│ Browser  │ ──────────────▶│  mcp-proxy   │ ──────────────▶│ upstream │
└──────────┘   with MITM    │              │                └──────────┘
                            │  ┌────────┐  │
                            │  │ CA cert│  │ ←── ~/.mcp-proxy/ca/cert.pem
                            │  └────────┘  │
                            │              │
                            │  ┌────────┐  │
                            │  │ scope  │──┼── in-scope  → record full body
                            │  │ guard  │  │
                            │  └────────┘  │── out-of-scope → record metadata only
                            │              │
                            │  ┌────────┐  │
                            │  │ SQLite │  │ ←── per-target requests.db
                            │  └────────┘  │
                            │              │
                            │  ┌────────┐  │
                            │  │  MCP   │──┼── 18 tools: read/search/diff/replay/fuzz/rewrite
                            │  └────────┘  │
                            └──────────────┘
```

### Storage layout

```
~/.mcp-proxy/
├── config.yaml                 # active project + target
├── audit.log                   # every MCP tool invocation (secrets redacted)
├── ca/
│   ├── cert.pem                # MITM CA — install in browser
│   └── key.pem                 # private (0600, never committed)
└── workspace/
    └── <project>/
        ├── meta.yaml
        └── targets/
            └── <host>/
                ├── meta.yaml
                └── requests.db
```

### Scope rules

When a request comes in, the proxy checks the URL host against the active target's `in_scope` and `out_of_scope` patterns:

- `out_of_scope` wins (a denylist inside an allowlist is still denied).
- Empty `in_scope` = allow whatever isn't denied.
- `*.<domain>` matches the domain and any subdomain.

**Example:** for a HackerOne program covering `*.empresa.com` but excluding the admin panel:

```yaml
in_scope:
  - "*.empresa.com"
out_of_scope:
  - "*.admin.empresa.com"
```

Browsing `https://api.empresa.com/users` is captured with body. `https://admin.empresa.com/` is captured with method/URL/headers only — no request or response body.

### Audit log redaction

Every MCP tool call writes a JSON line to `~/.mcp-proxy/audit.log`. Values of keys matching `password`, `passphrase`, `privatekey`, `api_key`, `secret`, `token`, `credential`, `authorization` are replaced with `"[redacted]"` before writing. Recursive into nested maps and slices.

```json
{"ts":"2026-08-01T12:34:56Z","tool":"get_active_context","detail":{"project":"H1-EMPRESA","password":"[redacted]"}}
```

---

## 🛠️ CLI reference

```text
mcp-proxy                                  # start MCP server on stdio
mcp-proxy project create --name N --type T # T: bugbounty | pentest
mcp-proxy project list
mcp-proxy project use NAME
mcp-proxy target add --host H --confirm
mcp-proxy target list
mcp-proxy target use HOST
mcp-proxy proxy [--addr :8080]            # MITM standalone (Ctrl+C to stop)
```

`mcp-proxy` with no arguments runs the MCP server — that's the mode Claude Desktop uses. `mcp-proxy proxy` runs the MITM proxy standalone; the two are designed to run side-by-side in different terminals.

---

## 🔒 Security

**This tool captures network traffic including authentication credentials. Operate only against systems you have explicit written authorization to test.**

- **CA is per-user, never committed.** `cert.pem` and `key.pem` are generated on first run, with `0600` permissions, and live under your home directory. Do not check them into git.
- **`add_target --confirm` is mandatory.** The CLI and the MCP tool both require an explicit confirmation token. There is no "force" or "auto" mode.
- **Out-of-scope bodies are not stored.** Only metadata (method, URL, headers) for hosts you didn't authorize.
- **Audit log is redacted.** Credentials are stripped before being written.
- **No telemetry.** No outbound connections. The binary does not phone home.

Read [docs/proxy.md](docs/proxy.md) for the threat model and the full PRD lives at [PRD-mcp-proxy-golang.md](PRD-mcp-proxy-golang.md).

---

## 🗺️ Roadmap

Done so far:

- ✅ **v1**: proxy MITM + CA + per-target storage + hard scope guard.
- ✅ **v2.0**: Workspaces (projects + targets), per-target storage, management MCP tools.
- ✅ **v2.1**: Read/replay tools (`list_requests`, `get_request_detail`, `search_bodies`, `replay_request`, `set_scope`), endpoint map, response diff & summarize.
- ✅ **v2.2**: Live match/replace (`set_match_replace`) and intruder-lite fuzzing (`fuzz_request`) — 18 MCP tools total.
- ✅ **v3.0**: Full Intruder (`intruder_start/status/results/cancel` with 4 attack types) + macro/session handling (`macro_record/play/list`) — 25 MCP tools total.
- ✅ **v4.0**: Passive scanner (`scan_passive_run/status`, `list_findings`, `get_finding_detail`, `finding_set_status`) + passive sitemap (`get_sitemap`) — 31 MCP tools total.
- ✅ **v4.1**: Active scanner (`scan_active_start/status`, non-destructive payloads, double opt-in via `MCP_PROXY_ACTIVE=1` + `confirmed=true`) + crawler (`crawl_start`, same-host, robots-aware) — 34 MCP tools total.
- ✅ **v5**: Decoder (`decode`/`encode`), comparer, extensions API, reports (markdown/html/pdf), tags/comments — 46 MCP tools total.
- ✅ **v5.1**: CTF tools (`export_curl`, `export_har`, `jwt_decode`, `jwt_resign`) + rich session briefing (`get_active_context`) + binary smoke test in CI — 50 MCP tools total.

All roadmap phases delivered. See MELHORIAS.md for future ideas.

---

## 🤝 Contributing

Bug reports and PRs welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first. For security issues, please disclose privately. See [MELHORIAS.md](MELHORIAS.md) for the improvement roadmap and technical debt assessment.

## 📄 License

[MIT](LICENSE) — see the file for full text. Copyright (c) 2026 Isaias Pereira.

> This project stands on the shoulders of [goproxy](https://github.com/elazarl/goproxy) (MIT), [mcp-go](https://github.com/mark3labs/mcp-go) (MIT), [cobra](https://github.com/spf13/cobra) (MIT), and [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (BSD). Thank you.
