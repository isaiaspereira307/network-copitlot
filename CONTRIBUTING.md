# Contributing to mcp-proxy

Thanks for your interest in contributing! This document covers the basics.

## Getting Started

1. Fork the repo and clone your fork.
2. Ensure you have **Go 1.25+** installed (`go version`).
3. Run tests to verify your setup:

```bash
go test ./...
```

## Development

### Building

```bash
go build -o mcp-proxy ./cmd/mcp-proxy
```

### Running tests

```bash
go test ./...              # all tests
go test -race ./...        # with race detector
go test -cover ./...       # with coverage
```

### Code style

- Follow standard Go conventions (`gofmt`, `go vet`).
- Keep packages under `internal/` — nothing here is importable outside this module.
- Each new MCP tool version goes in its own file: `internal/mcpserver/tools_vN.go` with corresponding `tools_vN_test.go`.
- Use the existing `audit.Log()` pattern for recording MCP tool calls.
- Prefer table-driven tests.

### Adding a new MCP tool

1. Define the tool in `internal/mcpserver/tools_vN.go`.
2. Register it in `RegisterTools()` in `server.go`.
3. Write tests in `tools_vN_test.go`.
4. Update `docs/COMMANDS.md` with the new tool reference.
5. Update the tools table in `README.md`.

## Submitting Changes

1. Create a feature branch from `main`.
2. Make your changes with clear, focused commits.
3. Ensure all tests pass: `go test ./...`
4. Open a PR against `main` with a clear description of what changed and why.

## Reporting Issues

Open a GitHub issue. Include:
- Go version (`go version`)
- OS and architecture
- Steps to reproduce
- Expected vs actual behavior

## Security

If you discover a security vulnerability, please **do not** open a public issue. Contact the maintainer directly for responsible disclosure.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
