# Memory Bank — Progress

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
