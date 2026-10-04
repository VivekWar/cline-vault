# Vault — Architecture & Conventions

This file holds the explanatory material that is deliberately kept out of
`.clinerules` (which contains hard rules only).

## What Vault is

Vault is a pure-Go (1.21+) MCP (Model Context Protocol) server that talks
stdio JSON-RPC 2.0. Its job is to detect AI-agent **context rot** in a
conversation/session and write a **handoff state file** so a fresh agent
session can resume cleanly.

## Protocol constraints (why the rules exist)

- **stdout is sacred.** The MCP transport writes JSON-RPC 2.0 messages to
  stdout. Any stray log line on stdout corrupts the stream and breaks the
  client. Therefore: every log, debug, warning, or error line goes to
  **stderr** without exception.
- **Notifications get no response.** JSON-RPC notifications (no `id`), such
  as the MCP `initialized` notification, must never be answered. Only
  requests (with `id`) get a response.
- **Deterministic detection only.** Context-rot detection uses local,
  deterministic heuristics (e.g. repeated-content scans, growing-revision
  loops, tool-error storms in the transcript). No external LLM calls, no
  network access — results must be reproducible and offline-safe.

## Planned layout (not yet implemented)

```
cmd/vault/          # main entrypoint; stdio JSON-RPC loop
internal/...        # protocol handling, heuristics, handoff writer
testdata/           # fixture stderr logs and sample JSON-RPC transcripts
bin/                # build output (git-ignored)
```

## Development workflow

- `make fmt-check` — fails if `gofmt -l .` reports any unformatted file.
- `make vet`      — `go vet ./...`
- `make test`     — `go test ./... -count=1`
- `make build`    — `go build -o bin/vault ./cmd/vault`
- `make verify`   — runs the four above in order, stops at first failure.

All targets skip gracefully when the module is empty/near-empty (no Go files
yet, no `cmd/vault` yet) so `make verify` is green from day one.

## Testing conventions

Every heuristic ships with **table-driven unit tests** fed by fixture inputs
under `testdata/` (fixture stderr logs, sample JSON-RPC transcripts).
Tests must be deterministic — no network, no wall-clock dependence, no
randomness without a fixed seed.

## Milestones

Work in small steps; propose a plan before large changes; commit after each
green milestone with a clear message. Progress is tracked in
`.cline/memory-bank/progress.md`.
