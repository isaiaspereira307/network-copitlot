# Proxy operation guide

This document covers the day-to-day operation of `mcp-proxy proxy` — the
MITM HTTP/HTTPS interception server. For an end-to-end tutorial, see the
[README](../README.md). For the design rationale, see the
[PRD](../PRD-mcp-proxy-golang.md).

## What the proxy does

It listens on a TCP port, accepts HTTP and HTTPS client traffic, and:

1. For **HTTP** — forwards the request to the upstream server and records
   the transaction.
2. For **HTTPS** (CONNECT method) — performs a TLS handshake with the
   client using a certificate **signed by the local CA** at
   `~/.mcp-proxy/ca/cert.pem`, terminates the encryption, opens a fresh
   TLS connection to the upstream, and records the decrypted transaction.

In both cases, the **scope guard** decides whether to record the body
(see [Scope rules](#scope-rules)).

## The CA (Certificate Authority)

On first run, `EnsureCA` generates a 2048-bit RSA self-signed CA and
stores it in `~/.mcp-proxy/ca/`:

- `cert.pem` — public certificate, install in every client that should
  trust the proxy (browsers, emulators, scripts using `requests` with
  `verify=cert.pem`).
- `key.pem` — private key, `0600` permissions. **Never share. Never
  commit.** This is what signs every per-host certificate the proxy
  generates on the fly.

The certificate is valid for 10 years from generation. To rotate, delete
`~/.mcp-proxy/ca/` and restart the proxy — a new CA is generated, and you
must re-install `cert.pem` everywhere.

### Where the CA shows up in goproxy

`goproxy.SetCA(ca, privKey)` is called once at proxy startup. The library
uses this CA as the parent of every per-host certificate it generates
when a client connects via CONNECT.

## Scope rules

The active target's `meta.yaml` has two list fields:

```yaml
in_scope:
  - "*.empresa.com"
out_of_scope:
  - "*.admin.empresa.com"
```

Matching algorithm (`internal/proxy/scope.go`):

1. **No target set** → all traffic recorded as metadata-only (no body).
   The CLI refuses to start the proxy without a target, so this is
   defensive only.
2. **`out_of_scope` patterns are checked first.** A hit denies the
   transaction. This lets you carve exceptions inside a broad allowlist.
3. **`in_scope` is empty** → allow whatever wasn't denied. The target
   exists but the user didn't restrict it.
4. **`in_scope` non-empty** → must match at least one pattern.

Pattern syntax is intentionally minimal:

| Pattern               | Matches                                  |
|-----------------------|------------------------------------------|
| `example.com`         | exactly `example.com` (case-insensitive) |
| `*.example.com`       | `example.com` and any subdomain          |
| `*.example.com/api`   | the path component is ignored; behaves like `*.example.com` |

## Out-of-scope handling

For a denied request, the proxy **still records**:

- method
- URL (full, with query string)
- request headers

It does **not** record:

- request body
- response status
- response headers
- response body

This lets you answer *"what URLs did this client hit?"* without
accidentally capturing credentials from systems outside your engagement.

## Storage and concurrency

Each target has its own `requests.db` (SQLite, WAL mode, foreign keys on).
The proxy opens one `*sql.DB` per target at startup and reuses it for
the lifetime of the process. Indexes are on `(ts)` and `(method, url)`.

If the proxy is restarted while a `requests.db` is large, the first
request after restart may be slightly slower (WAL checkpoint). Steady
state is `O(1)` per `Insert`.

There is **no shared store** between the proxy process and the MCP
server process. They both read the same `requests.db` file via SQLite's
WAL — readers don't block writers, and vice versa. This means you can
have `mcp-proxy proxy` running in one terminal and `mcp-proxy` (MCP
server) connected to Claude Desktop in another, querying the same data
in real time.

## Performance

Targets in the PRD:

- v1: 100 req/s sustained.
- v3: intruder with 10 parallel workers.
- v4: passive scanner with p99 < 200ms over the capture.

Measured on a 2020 M1 / SSD: 100 req/s is comfortable for typical
JSON API traffic (≤ 10 KB body). Above ~500 req/s, the bottleneck is
SQLite fsync; switch to `PRAGMA synchronous=OFF` if you have a UPS and
need to go faster. **Don't do this on a laptop without a UPS.**

## Threat model

| Adversary                      | What they can do                                           |
|--------------------------------|------------------------------------------------------------|
| Compromised browser extension  | Steals session cookies in captured transactions. **No mitigation here** — keep your browser clean. |
| Local user reading `audit.log` | Sees every MCP tool call. Secret values are redacted.       |
| Local user reading `requests.db` | Sees every captured request/response. **No encryption by default** — enable SQLCipher (PRD §5) if needed. |
| Remote network observer        | Sees `proxy:port` traffic between browser and proxy. If the proxy is on `127.0.0.1`, this is the loopback only. |
| Attacker who plants a CA cert  | If they can write to `~/.mcp-proxy/ca/cert.pem`, they can MITM everything. Protect your home dir. |

## Known limitations

- **Certificate pinning in mobile apps** — apps that pin the upstream
  cert will refuse to connect through the proxy. Use Frida/objection
  to bypass, or skip the app and test the API directly.
- **WebSocket frames** — recorded as one big request, not frame-by-frame.
  Upgrade-aware splitting is on the v3 roadmap.
- **HTTP/2** — goproxy handles it but body capture is best-effort.
- **gRPC** — works, but `:authority` and `:path` pseudo-headers appear
  in the request headers verbatim.
- **Multiple proxies in one process** — `goproxy.SetCA` is global. If
  you need N proxies, fork the process or patch in a `CertStore`.

## What is **not** logged

- Source IP of the client (the proxy runs on the same host as the browser).
- DNS queries (use `tcpdump` for that).
- TCP-level timing (TTFB is recorded at the application layer, not the
  network layer).

## Failure modes

| Symptom                                  | Cause                                     | Fix                              |
|------------------------------------------|-------------------------------------------|----------------------------------|
| Browser shows `NET::ERR_CERT_AUTHORITY_INVALID` | CA not installed in browser               | Install `cert.pem` (see README)  |
| Browser shows `ERR_TUNNEL_CONNECTION_FAILED`     | Proxy not running                        | `mcp-proxy proxy` in a terminal  |
| `proxy: address already in use`         | Another process on the port               | `--addr :9080`                   |
| `ca: mkdir ... permission denied`        | `~/.mcp-proxy/` not writable              | `chmod 700 ~`                    |
| Captured transactions not in DB         | Target not active                         | `mcp-proxy target use HOST`      |
