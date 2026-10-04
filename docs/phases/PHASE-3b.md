# Vault — Phase 3b Report (Telemetry Architecture Fix)

## Summary

The Phase 3 plugin had two architectural flaws: it wrote `.vault/activity.jsonl`
directly (bypassing the Go server, so `heuristics.Snapshot()` never ran and the
`tree` field — which H2 churn depends on — was always missing), and it ignored
file edits (fixed previously and retained here).

The fix routes every intercepted activity through the Go server:

- **Go side:** new CLI subcommand `vault report [--root DIR] '<json-args>'` in
  `cmd/vault/main.go`, which feeds the payload to
  `state.DispatchTool("report_activity", ...)` — the exact code path the MCP
  tool uses, including the git snapshot — prints the tool result, and exits 1
  on tool-level errors. `health` was refactored onto a shared `absRoot`
  helper. `bin/vault` rebuilt.
- **Plugin side:** `index.ts` no longer writes files. It builds a payload
  matching `reportActivitySchema` (`kind`, `command`, `exit_code`, `stderr`
  truncated to 40 lines, `files`, `workspace`; the server adds `time`) and
  spawns the vault binary via `spawnSync(binary, ["report", "--root",
  workspaceRoot, payload])`. The binary is located via `VAULT_BIN`, else
  `<workspace>/bin/vault`, else PATH. Spawn failures are logged to stderr and
  swallowed (observational hook).

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| Direct file writing removed (fs.appendFileSync/buildLine gone) | PASS | `index.ts` diff |
| `vault report '<json>'` subcommand calls report_activity | PASS | `TestReportCLI` (2 subtests) |
| Plugin spawns the Go binary via spawnSync | PASS | `report()` in `index.ts` |
| Payload matches reportActivitySchema | PASS | `buildPayload` unit tests |
| COMMAND and EDIT branches preserved | PASS | existing + new unit tests, smoke test |
| Activity lines now carry the `tree` field | PASS | architecture smoke test: two 40-char tree hashes |
| TS compiles | PASS | `npm run typecheck` clean |
| Plugin tests | PASS | 19/19 |
| `make verify` green | PASS | output below |

## Files built

- `cmd/vault/main.go` (117) — `report` subcommand, shared `absRoot`, dispatch switch.
- `cmd/vault/main_test.go` (167) — `TestReportCLI` (valid payload → "recorded #1" + line written; invalid kind → exit 1).
- `.cline/plugins/vault-telemetry/index.ts` (253) — spawn-based `report()`, `buildPayload`, `vaultBinary`; file writing removed.
- `.cline/plugins/vault-telemetry/index.test.ts` (143) — `buildPayload` tests replace the removed `buildLine` tests.

## Design decisions

- **`--root` is passed explicitly** (in addition to the payload's `workspace`
  field): the plugin process environment cannot be trusted to have VAULT_ROOT
  set, and its cwd may not be the workspace, so both the state location and
  the snapshot directory are pinned to `workspaceRoot`.
- **`files: [filePath]` on EDIT entries** (retained from the prior patch):
  keeps `create_handoff`'s Touched Files section populated; schema-compliant.
- **`stderr` still truncated to the last 40 lines** before the payload is sent,
  matching the Phase 2 `.clinerules` convention and bounding payload size.
- **30 s spawn timeout** so a slow git snapshot cannot hang the hook forever;
  all failures are logged and swallowed.

## Deviations from the prompt

- The task's example invocation `vault report '<json>'` was extended with an
  optional `--root` flag (defaulting to VAULT_ROOT/cwd, like `health`) and the
  plugin passes it explicitly for deterministic behavior.

## Problems encountered

- None in the implementation. (Smoke-test script bug: `execSync` imported from
  `node:fs` instead of `node:child_process` — test harness only, fixed.)

## Test evidence

- `go test ./cmd/vault -run TestReportCLI -v` — 2/2 subtests pass.
- Plugin: `npm run typecheck` clean; `npm test` 19/19 pass.
- Architecture smoke test (real temp git repo + real Go binary): one
  `run_commands` and one `write_to_file` afterTool call → two lines in
  `<repo>/.vault/activity.jsonl`, both with 40-char `tree` hashes, correct
  kind/command/exit_code/workspace — proving snapshots now happen server-side.

Full `make verify` output:

```
OK   fmt-check (nothing to format)
OK   vet
ok  	vault/cmd/vault	0.762s
ok  	vault/internal/heuristics	0.207s
ok  	vault/internal/mcp	0.015s
ok  	vault/internal/state	0.103s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

- `npm run typecheck` — clean.
- Architecture smoke test via a standalone Node script (see above).

## Known limitations

- One Go process spawn per tool call; snapshot cost grows with repo size.
- Cline plugins still only load in the SDK/CLI/Kanban hosts.

## Handoff to next phase

Phase 4: verify live telemetry after the plugin loads (commands + edits, tree
fields present), then demo-tuning of H1/H2 thresholds against real telemetry.

## Metadata

- Commit: `61d9850` (`feat: phase 3b telemetry architecture fix (report subcommand, server-side snapshots)`)
- Tag: `phase-3b-done` (re-tagged)
- Branch: `cline/cbf4d`

## USER ACTION REQUIRED

- Restart Cline / start a new session in this workspace so the host loads the
  updated plugin. Everything else (compile, tests, smoke test, commit, merge,
  rebuild, settings check, log backup) was completed.
