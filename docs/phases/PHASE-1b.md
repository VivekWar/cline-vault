# PHASE 1b — Worktree-awareness, read_handoff, activity rotation

Status: DONE.

## Summary

Made Vault worktree-aware: an optional `workspace` argument (absolute path) on
`report_activity`, `check_context_health` and `create_handoff` so git runs in
the agent's worktree rather than the main checkout; added `read_handoff` so a
fresh task can resume from the shared `VAULT_ROOT/.vault`; and rotated
`activity.jsonl` into `.vault/archive/` after each successful handoff so the
next session starts clean. `.clinerules` telemetry rules were tightened. No
heuristics were implemented (health is still a stub).

## Acceptance criteria table

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| make verify green | PASS | output below |
| Commit "feat: worktree-aware workspace arg, read_handoff, activity rotation" + tag phase-1b-done | PASS | commit 2299469, tag phase-1b-done |
| ff-merge main + rebuild main bin/vault | PASS | "Already up to date" (committing on main); `make build` OK |
| PHASE-1 addendum (manual verification + fixes) | PASS | docs/phases/PHASE-1.md |
| progress.md updated | PASS | .cline/memory-bank/progress.md |

## Files built (purpose + line count, wc -l)

| File | Lines | Purpose |
|---|---|---|
| internal/state/state.go | 183 | `workspace` on activity entries; `read_handoff`; `check_context_health` workspace arg; stderr logger |
| internal/state/handoff.go | 282 | `create_handoff` workspace + rotation; `gitState`/`gitDir`/`isGitRepo`; `buildHandoff` title/_none_ |
| internal/state/state_test.go | 322 | tests: workspace git, fallback, read_handoff, rotation, title/empty sections, workspace storage |
| internal/mcp/tools.go | 108 | `workspace` in schemas; 4th tool `read_handoff` |
| internal/mcp/mcp_test.go | 252 | transcript test updated to 4 tools |
| cmd/vault/main_test.go | 88 | e2e test updated to 4 tools |
| .clinerules | 35 | telemetry + ff-merge + end-of-phase checklist rules |
| docs/PHASE_CHECKLIST.md | 50 | end-of-phase checklist (this follow-up) |

## Design decisions

- **`workspace` is optional and stored per activity entry.** Git must reflect
  the agent's worktree, so `create_handoff` resolves git dir from it.
- **`gitDir` fallback**: use `workspace` only when it is an absolute path AND
  inside a git repo; otherwise fall back to `VAULT_ROOT` and log the reason to
  stderr — never error.
- **Rotation after a successful handoff write only**; `"activity archived to:
  none"` when there is nothing to archive (no archive dir created).
- **`read_handoff` returns full handoff text**, or `isError:true` "no handoff
  yet" when absent.
- **Handoff format**: `# Vault Handoff` title + resume line first; empty
  sections render `_none_`; git section has workspace/branch/commit/status.
- **Rejected alternatives**: storing `.vault` in the worktree (rejected — the
  shared `VAULT_ROOT/.vault` survives worktree deletion); always running git in
  `VAULT_ROOT` (rejected — misses the agent's edits); deleting `activity.jsonl`
  instead of archiving (rejected — loses the audit trail).

## Deviations from the prompt

- **Tests were written first** (TDD): yes — red phase confirmed (undefined
  `State`/`NewServer`/`gitDir`/`readHandoff` etc.) before implementation.
- Rotation amendment applied (rotate only after successful write; `none` case
  tested via `TestActivityRotation/no_activity`).
- Workspace amendment applied (absolute + git-repo check; relative-path case in
  `TestGitDirFallback`).
- ff-merge amendment applied (`--ff-only` only; never force/rebase/reset).
- No other deviations from the approved plan.

## Problems encountered

- TDD red phase showed undefined symbols until implementation landed — expected.
- ff-merge was a no-op ("Already up to date") because this session commits
  directly on `main` (the main checkout), not a `cline/<id>` worktree branch —
  noted, not a failure.
- No failed attempts; no code breakage in this phase.

## Test evidence

`go test ./... -v -count=1` summary (all PASS; 3 packages, 15 top-level tests
plus subtests — TestErrorCases×3, TestActivityRotation×2):

```
--- PASS: TestEndToEnd
--- PASS: TestServeTranscript
--- PASS: TestInitializeDefaultProtocolVersion
--- PASS: TestPingResultEmptyObject
--- PASS: TestErrorCases (malformed_json, unknown_method, create_handoff_missing_goal)
--- PASS: TestOversizedLine
--- PASS: TestReportActivityCounts
--- PASS: TestCreateHandoff
--- PASS: TestCreateHandoffMissingGoal
--- PASS: TestCreateHandoffUsesWorkspaceGit
--- PASS: TestGitDirFallback
--- PASS: TestReadHandoff
--- PASS: TestActivityRotation (with_activity, no_activity)
--- PASS: TestHandoffTitleAndEmptySections
--- PASS: TestReportActivityStoresWorkspace
```

Full `make verify` output:

```
OK   fmt-check (nothing to format)
OK   vet
ok  	vault/cmd/vault	0.243s
ok  	vault/internal/mcp	0.014s
ok  	vault/internal/state	0.087s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

Reported by the user (pre-fix findings):

- MCP server registered with **3 tools**; a live tool call succeeded.
- **Resume test FAILED** because of the worktree path mismatch (fresh task
  couldn't find `.vault/handoff_state.md`; git in `VAULT_ROOT` didn't see the
  agent's edits).
- **Telemetry risk test**: 6 entries logged for 5 actions — calls were batched
  (identical timestamps), and one chained command hid a failure (reported
  `exit_code 0`).

Post-fix, the deployed binary (`bin/vault`) reports exactly 4 tools:
`report_activity`, `check_context_health`, `create_handoff`, `read_handoff`.

## Known limitations

- `check_context_health` is still a stub (always score 100).
- `read_handoff` reads the single shared handoff; no per-worktree handoffs.
- Archive grows unbounded (no retention/cleanup yet).
- `workspace` is validated only for "absolute + inside a git repo", not that it
  is the *current* worktree.

## Handoff to next phase

Phase 2: replace the `check_context_health` stub with real deterministic,
offline context-rot heuristics (repeated content, revision loops, tool-error
storms), backed by `testdata/` fixtures and table-driven tests.

## Metadata

- Branch: `main`
- Tag: `phase-1b-done` (force-moved by this checklist run)
- Commit: `2299469` (feat) + `a0aa3e4` (docs addendum); this report is
  committed by the checklist run.

## USER ACTION REQUIRED

Reload the MCP server (restart Cline or its MCP connection) so the updated
settings take effect. I edited and validated
`/home/vivek/.cline/data/settings/cline_mcp_settings.json` (vault entry:
`disabled=false`, `autoApprove` = report_activity, check_context_health,
create_handoff, read_handoff; backup at `cline_mcp_settings.json.bak`), but I
cannot trigger Cline's live reload from here.

