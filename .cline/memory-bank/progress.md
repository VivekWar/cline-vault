# Memory Bank — Progress

## 2026-10-04 — Phase 1: stdio MCP server skeleton

**Status: DONE** — `make verify` green; committed as
"feat: phase 1 stdio MCP server skeleton", tag `phase-1-done`.

Built:
- `cmd/vault/main.go` — stdio entrypoint; VAULT_ROOT (abs) else cwd; no
  `.vault/` pre-creation; logs to stderr only.
- `internal/mcp` — `bufio.Reader.ReadBytes('\n')` loop, 4 MB line cap
  (oversized → -32700 id null, keep serving); raw id echo; notifications
  never answered; ping → `{"result":{}}`; tools/list + tools/call
  (isError:true for tool failures); initialize echoes protocolVersion
  (default "2025-06-18").
- `internal/state` — `report_activity` (appends RFC3339 JSON line,
  "recorded #N"), `check_context_health` STUB (score 100), `create_handoff`
  (8 H2 sections in order, deduped touched files, last-3-failing stderr,
  git state or "git unavailable", atomic temp+rename write).
- Tests (10, all passing): transcript/4-lines/3-tools, initialize default,
  ping non-null + string id, table-driven error cases (-32700/-32601/
  isError + keep-serving), oversized line, activity counts + on-demand
  .vault, handoff headings/dedup, e2e binary test.

Docs: `docs/phases/PHASE-1.md`. Cline MCP config snippet in phase report.

Next: Phase 2 candidates — implement actual context-rot heuristics behind
`check_context_health` (deterministic, offline), wire testdata fixtures.


**Status: DONE** (no feature code)

- Verified project repo isolation: parent repo `/home/vivek` tracks/stages
  nothing from this project (commit scare left zero contamination).
- `.gitignore` extended: `.vault/`, `coverage.out`, `*.out`, `*.test`.
- Makefile skip logic tightened: vet/test/build SKIP only on zero `.go` files
  (reason printed); otherwise run for real and fail on error — proven with a
  temporary failing test (`make verify` exit=2), then cleaned up.
- `.clinerules`: added go.mod stays at `go 1.21`, no newer language features.
- `docs/phases/PHASE-0.md` written (goal, decisions, problems, evidence).
- Commit "chore: phase 0 hardening and documentation", tag `phase-0-done`.

Next: implement minimal stdio JSON-RPC 2.0 loop in `cmd/vault` (respond to
`initialize`, no response to `initialized`, all logs to stderr), then
`make verify`.

## 2026-10-04 — Dev environment setup

**Status: DONE** (environment only; no Vault features implemented)

What exists now:
- `go.mod` (module `vault`, go 1.21), `Makefile` (fmt-check, vet, test, build,
  verify — all tolerate an empty/near-empty module), `.gitignore` (`bin/`).
- `.clinerules` — short hard-rule set (<40 lines).
- `ARCHITECTURE.md` — explanatory material, protocol constraints, planned
  layout, workflow.
- `testdata/README.md` — scaffold for fixture stderr logs and JSON-RPC
  transcripts.
- Fresh git repo in this folder (isolated from the enclosing home-dir repo),
  commit: "chore: dev environment setup (Makefile, tightened clinerules)".

Notes:
- Go 1.24.0 toolchain installed (go.mod targets 1.21). git user configured
  (VivekWar). node v24 / npx 11 present but unused.
- `make build` currently SKIPS because `cmd/vault` does not exist yet.

Next task (suggested first command): `mkdir -p cmd/vault` then implement the
minimal stdio JSON-RPC 2.0 loop (respond to `initialize`, send no response to
`initialized`, log only to stderr), then `make verify`.
