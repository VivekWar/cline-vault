# Vault — Phase 6 Report

## Summary

Phase 6 makes Vault installable with one command and pushes it to GitHub:

1. **One-command install.** `make install` → `scripts/install.sh`, which:
   verifies Go ≥ 1.21 and git (clear error otherwise); builds `bin/vault`;
   registers the `vault` MCP server in Cline's `cline_mcp_settings.json`
   (backup first, idempotent merge, never clobbers other servers, Linux/
   macOS path detection, `CLINE_MCP_SETTINGS` override); runs `npm ci` in
   `.cline/plugins/vault-telemetry` when node exists (skip note otherwise);
   appends the context-health hook to `.clinerules` if absent; smoke-tests
   with `bin/vault health`; prints "Vault installed. Restart Cline."
   `make uninstall` removes only the MCP entry.
2. **JSON merge in Go.** A hidden `vault mcp-register` subcommand
   (`cmd/vault/mcp_register.go`) does the settings merge — no python/jq
   dependency; unit tests run against temp settings files so real settings
   are never touched by tests.
3. **README.md** — title/pitch, problem, copy-paste quickstart, ASCII data
   flow, detection (H1/H2), tools table, extras, honesty note, Cline credit.
4. **Push** — everything merged into `main` and pushed to
   `https://github.com/VivekWar/cline-vault.git` with tags (see Deployment).

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| `make install` one command | PASS | manual run below (temp HOME) |
| Go ≥ 1.21 + git verified with clear error | PASS | `install.sh` prereq block (sort -V comparison) |
| Builds bin/vault | PASS | `==> building bin/vault` |
| MCP registration: backup, idempotent merge, other servers preserved | PASS | `TestRegisterMCPSetsVaultEntry`, `TestRegisterMCPIdempotentPreservesOthers`, manual reinstall → `.bak-*` |
| Path detection Linux/macOS + env override | PASS | `detect_settings` in install.sh; `CLINE_MCP_SETTINGS` honored |
| npm ci only when node exists | PASS | node present → ran; skip note branch implemented |
| .clinerules hook added if absent | PASS | manual run appended the line; re-run is a no-op |
| `bin/vault health` smoke test + final message | PASS | manual run output |
| `make uninstall` removes only the vault entry | PASS | `TestUnregisterMCPRemovesOnlyVault`, manual run left `"mcpServers": {}` |
| Installer tested against temp HOME | PASS | manual run with `HOME=$T CLINE_MCP_SETTINGS=$T/...`; Go tests use `t.TempDir()` |
| README with the 9 required sections | PASS | file |
| Pushed to GitHub (main + tags) | PASS/FAIL | see Deployment section |
| `make verify` green | PASS | output below |

## Files built

- `scripts/install.sh` (105) — installer/uninstaller entrypoint.
- `cmd/vault/mcp_register.go` (183) — `mcp-register` subcommand +
  `registerMCP`/`unregisterMCP`/backup/write helpers.
- `cmd/vault/mcp_register_test.go` (170) — 4 unit tests on temp settings
  files.
- `Makefile` (54) — `install`/`uninstall` targets.
- `README.md` (93) — user-facing docs.
- `cmd/vault/main.go` (+3) — `mcp-register` case.
- `.clinerules` (+1) — hook line (added by the installer run).

## Design decisions

- **JSON merge lives in Go** (`go run ./cmd/vault mcp-register` for
  uninstall, built binary for install), so the installer depends only on
  bash + Go — both already required — and the merge is unit-testable.
- **Backups are second-timestamped with a numeric suffix** on collision, so
  two installs in the same second never overwrite each other's backups.
- **Idempotency by construction:** only `mcpServers["vault"]` is read/
  written; everything else round-trips through a generic `map[string]any`.
- **Entry shape matches what this Cline host actually uses** (`transport`
  block + top-level `autoApprove` + `disabled: false`).
- **Uninstall is a no-op when no settings file exists** (never creates one).
## Deviations from the prompt

- README "Extras" lists redaction, compression estimate and `vault report`
  but **not `vault serve`** (removed earlier per user instruction).
- README "Honesty note" describes the `demo-trap` (scripted broken module
  from Phase 5) and the SDK/CLI/Kanban host limitation of the telemetry
  plugin.
- The `main` fast-forward in the phase checklist could not run: `main` had
  diverged (parallel Phase 5 session). Per the explicit Phase 6 instruction
  to push `main`, a **non-ff merge** was performed instead (no force, no
  rebase, no reset) — see Deployment.
- Tests for the installer's shell parts are manual (temp HOME run recorded
  below); the JSON merge itself is fully unit-tested in Go.

## Problems encountered

- **`filepath.Abs(os.Executable())` type error** (Executable returns two
  values) — fixed by unpacking first.
- **Backup name collision within the same second** broke the idempotency
  test — fixed with a numeric suffix loop in `backupSettings`.
- **Unregister created a settings file when none existed** — fixed to a true
  no-op.
- All caught by `make verify` before commit.

## Test evidence

`go test ./... -v -count=1` — all packages green, including
`TestRegisterMCPSetsVaultEntry`, `TestRegisterMCPIdempotentPreservesOthers`,
`TestUnregisterMCPRemovesOnlyVault`, `TestUnregisterMCPMissingFileIsNoOp`.

Full `make verify` output:

```
OK   fmt-check (nothing to format)
OK   vet
ok 	vault/cmd/vault	0.801s
ok 	vault/internal/heuristics	0.355s
ok 	vault/internal/mcp	0.013s
ok 	vault/internal/state	0.173s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification (temp HOME — real settings untouched)

```
$ HOME=$T CLINE_MCP_SETTINGS=$T/cline_mcp_settings.json ./scripts/install.sh
==> building bin/vault
==> registering vault MCP server in /tmp/.../cline_mcp_settings.json
registered vault MCP server in /tmp/.../cline_mcp_settings.json
==> installing telemetry plugin dependencies (npm ci)
==> added context-health hook to .clinerules
==> smoke test: bin/vault health
{"score":100,"status":"healthy",...}
Vault installed. Restart Cline.
```

- Settings file: vault entry with transport(command=abs bin/vault, cwd=root,
  env VAULT_ROOT=root), autoApprove = 4 tools, disabled=false.
- Reinstall → backup file `.bak-20261004T122205Z` created; other servers
  preserved (Go test).
- Uninstall → vault entry removed, `"mcpServers": {}` left behind.

## Deployment (merge + push)

- `main` (Documents checkout, with Phase 5) and `cline/d7a73` (Phase
  4b/4c-removal/6) diverged at `4bd18f0`; `origin/main` was at `942c645`.
  Merged `cline/d7a73` into `main` with `--no-ff` (conflicts in
  `.cline/memory-bank/progress.md` and `.clinerules` resolved by combining
  both entries; merge commit `9c285b9`).
- Deployed binary rebuilt, `tools/list` re-checked (4 tools), MCP settings
  verified (the real `make install` registered the deployed binary path,
  backup `.bak-20261004T122406Z`), logs backed up to
  `~/cline-log-backups/phase-6-20261004-1754` (107M).
- **Push executed:** `git push -u origin main --tags` →
  - `main`: `942c645..9c285b9` pushed, upstream set ✓
  - `phase-5-done` (1dd8530): new tag pushed ✓
  - `phase-6-done` (18992cd): new tag pushed ✓
  - `phase-4-done`: rejected — the tag already exists on origin (pointing
    at `942c645`, an ancestor of the new main). Updating it would require a
    force-push, which is forbidden by `.clinerules`; left untouched and
    reported. The code itself (web UI removed) is in `main` regardless.


## Known limitations

- macOS VS Code path is probed but not empirically verified on this Linux
  box (cannot test here).
- The installer is bash-dependent (fine for the stated Linux/macOS targets).
- `npm ci` failure is a warning, not an error — the plugin is optional.

## Handoff to next phase

The repo is installable end-to-end: `git clone && cd && make install &&
restart Cline`. Remaining nice-to-haves: CI (GitHub Actions running
`make verify`), version pinning of the telemetry plugin.

## Metadata

- Commit: `14dfeca` (feature); docs/tag follow.
- Tag: `phase-6-done`
- Branch: `cline/d7a73`, merged into `main` (non-ff).

