# mcp-proxy

> MITM HTTP/HTTPS proxy + MCP server for AI-assisted bug bounty and pentesting.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-compatible-purple)](https://modelcontextprotocol.io)
[![Platform: Linux/macOS/Windows](https://img.shields.io/badge/platform-linux%20%7C%20macOS%20%7C%20windows-lightgrey)]()

A native-MCP interception proxy. Point your browser at it, browse your authorized target, and an AI assistant (Claude) manages projects/targets over MCP; reading and replaying captured transactions lands in Sprint 1.

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
- **MCP tools** to manage projects/targets (list/inspect/replay of transactions lands in Sprint 1).
- **Hard scope guard** — out-of-scope traffic is logged without bodies.
- **Zero CGO, zero runtime deps** — single static binary.
- **Audit log** with automatic redaction of secrets (`password`, `token`, `api_key`, etc).

---

## 📦 Installation

### From source (requires Go 1.25+)

```bash
git clone https://github.com/isaias/network-copitlot
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

| Tool                  | What it does                                |
|-----------------------|---------------------------------------------|
| `create_project`      | Create a new engagement                     |
| `list_projects`       | List all engagements                        |
| `set_active_project`  | Switch the active project                   |
| `add_target`          | Add a target (requires `confirmed: true`)   |
| `list_targets`        | List targets in the active project          |
| `set_active_target`   | Switch the active target                    |
| `get_active_context`  | Show current project, target, request count |

Then in Claude:

> *"What's the current project and target?"*

Claude calls `get_active_context`, which returns the active project, target, and stored request count. Reading and replaying individual transactions (`list_requests`, `get_request_detail`, `replay_request`, `search_bodies`) is not shipped yet — it lands in Sprint 1 of the current plan (see Roadmap below).

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
                            │  │  MCP   │──┼── management tools; list/replay in Sprint 1
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

This is **v2.x** of the project. Done so far:

- ⚠️ **v1 (parcial)**: proxy MITM + CA + storage por target; tools de leitura/replay (`list_requests`, `get_request_detail`, `replay_request`, `set_scope`, `search_bodies`) pendentes — Sprint 1 do plano.
- ✅ **v2.0**: Workspaces (projects + targets), per-target storage, 7 MCP tools for management.

Coming next (see PRD for full list):

- 🔜 **v2.1** — Match & Replace on-the-fly, custom request editor.
- 🔜 **v3** — Intruder fuzzing engine, macro/session handling.
- 🔜 **v4** — Passive scanner (reflected XSS, IDOR, SQLi, SSRF, secrets in JS) + sitemap.
- 🔜 **v5** — Decoder, comparer, extensions API (Go plugins).

---

## 🤝 Contributing

Bug reports and PRs welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first (TBD — repo is new). For security issues, please disclose privately.

## 📄 License

[MIT](LICENSE) — see the file for full text. Copyright (c) 2026 Isaias Pereira.

> This project stands on the shoulders of [goproxy](https://github.com/elazarl/goproxy) (MIT), [mcp-go](https://github.com/mark3labs/mcp-go) (MIT), [cobra](https://github.com/spf13/cobra) (MIT), and [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (BSD). Thank you.
