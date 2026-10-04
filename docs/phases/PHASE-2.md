# Vault — Phase 2 Report

## Summary

Phase 2 implements the first two deterministic, offline context-rot detection
heuristics plus the health aggregator:

- **H1 — Recurring error loop (Jaccard).** `Normalize` reduces command/test
  output to a de-duplicated, lowercase token set after stripping file paths,
  hex addresses, RFC3339/HH:MM:SS timestamps, durations, bare numbers, and Go
  test boilerplate. `DetectErrorLoop` flags `RECURRING_ERROR_LOOP` when the
  last 3 `COMMAND`/`TEST` entries all fail with non-empty output and every
  pairwise Jaccard similarity is >= 0.70.
- **H2 — Code oscillation (net-to-gross churn).** `Snapshot` computes an exact
  git tree hash of the working tree without touching the real index (temp
  `GIT_INDEX_FILE` + `git add -A` + `git write-tree`). `Churn` sums per-pair
  `git diff --numstat` into gross and first-to-last into net; `DetectOscillation`
  flags `CODE_OSCILLATION_THRASHING` when gross > 100 and net/gross < 0.15.
- **Aggregator.** `Assess` returns a `Health` JSON (score/status/flags/reason/
  recommendation/metrics) with `-50` and `-35` penalties and a reserved
  `TOOL_CALL_TRAP` slot for Phase 3.

`check_context_health` now returns the real Health JSON, and `vault health
[--root DIR] [--workspace DIR]` prints the same JSON to stdout.

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| Normalize strips paths/numbers/durations/timestamps/hex/boilerplate | PASS | `TestNormalize` (8 cases) |
| Jaccard empty→0, identical→1, disjoint→0, partial | PASS | `TestJaccard` |
| DetectErrorLoop flags loop fixtures, not progress, success breaks loop | PASS | `TestDetectErrorLoop` |
| numstat parsing incl. binary `-` and gross=0 ratio | PASS | `TestParseNumstat`, `TestChurnEfficiency` |
| Oscillation ratio/threshold logic | PASS | `TestDetectOscillation` |
| Churn integration: oscillation flags, progress no-flag, index untouched | PASS | `TestChurnIntegration` |
| Snapshot works in a git worktree | PASS | `TestSnapshotInWorktree` |
| Assess score/status/flag combinations | PASS | `TestAssess` |
| MCP-level check_context_health on loop fixtures → degraded | PASS | `TestCheckContextHealthLoopFlag` |
| CLI `vault health --root` prints valid JSON | PASS | `TestHealthCLI` |
| `make verify` green | PASS | output below |

## Files built

- `internal/heuristics/heuristics.go` (212) — H1: Normalize, Jaccard, DetectErrorLoop, types.
- `internal/heuristics/churn.go` (205) — H2: Snapshot, Churn, parseNumstat, churnEfficiency, DetectOscillation.
- `internal/heuristics/assess.go` (129) — Health/Metrics/Flag types, thresholds + env overrides, Assess.
- `internal/heuristics/heuristics_test.go` (356) — table-driven pure unit tests.
- `internal/heuristics/churn_integration_test.go` (141) — git integration tests.
- `internal/state/health_test.go` (74) — MCP-level health test.
- `testdata/heuristics/{loop_same_assertion_1,2,3}.txt` (5 each) — loop fixtures.
- `testdata/heuristics/{progress_1,2,3}.txt` (4/7/5) — progress (false-positive) fixtures.

## Design decisions

- **Snapshot via temp index copy.** `git rev-parse --git-path index` locates the
  real (per-worktree) index; we copy it to a temp file, set `GIT_INDEX_FILE` to
  that temp, then `git add -A` + `git write-tree`, then delete the temp. This
  snapshots the working tree exactly as `git add -A && git write-tree` would,
  without mutating the caller's staged index — proven by
  `TestChurnIntegration/snapshot_leaves_index_unchanged`. Rejected alternative:
  running `git add -A` directly on the real index (destroys the user's staging
  state).
- **Boilerplate stripping in Normalize.** Lines like `=== RUN`, `--- PASS`,
  `PASS`, `FAIL`, `ok  `, `exit status N`, and `FAIL\t<pkg>` are identical
  across *every* failing run regardless of the actual error. Leaving them in
  would inflate Jaccard similarity for unrelated failures and cause false
  positives, so they are dropped before tokenization so the signal comes from
  the actual error text.
- **Git execution behind replaceable seams.** `snapshotFn` and `diffNumstatFn`
  are package-level function variables; tests replace them to keep the pure
  arithmetic (numstat parsing, ratio) testable without a repo.

## Deviations from the prompt

- `Churn` and `Assess` take a leading `workspace string` argument (the spec
  wrote `Churn(trees []string)` / `Assess(entries, trees)`), because
  `git diff --numstat <t1> <t2>` requires a repository context. Documented in
  code comments.
- Tests were written first: fixtures + all test files were created and run
  (shown failing) before any implementation code was added.
- "A loop broken by a success in the middle" is constructed inline in
  `TestDetectErrorLoop` rather than a fixture file, since `DetectErrorLoop` is
  a pure function.

## Problems encountered

- **Snapshot returned "" in a plain temp repo.** `git rev-parse --git-path index`
  returns a *relative* path (`.git/index`) in non-worktree repos, but the index
  copy used `os.ReadFile` relative to the Go process cwd (the package dir), so
  the copy silently failed. Fix: resolve a relative index path against the
  workspace (`filepath.Join`) before copying. Root cause: assuming
  `--git-path` is always absolute; it is absolute in worktrees but relative in
  plain repos.

## Test evidence

`go test ./... -v -count=1` summary: all packages `ok` —
`vault/cmd/vault` (TestEndToEnd, TestHealthCLI), `vault/internal/heuristics`
(TestChurnIntegration, TestSnapshotInWorktree, TestNormalize, TestJaccard,
TestDetectErrorLoop, TestParseNumstat, TestChurnEfficiency,
TestDetectOscillation, TestAssess), `vault/internal/mcp` (5 tests),
`vault/internal/state` (TestCheckContextHealthLoopFlag + 10 existing).

Full `make verify` output:

```
OK   fmt-check (nothing to format)
OK   vet
ok  	vault/cmd/vault	0.442s
ok  	vault/internal/heuristics	0.179s
ok  	vault/internal/mcp	0.010s
ok  	vault/internal/state	0.081s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

- `./bin/vault health --root <tmp>` prints valid Health JSON (score 100, healthy).
- End-to-end binary transcript: 3 failing TEST report_activity calls followed by
  check_context_health returned score 50, status degraded, flag
  RECURRING_ERROR_LOOP with similarities [1,1,1] and recommendation
  "call create_handoff and start a fresh task".

## Known limitations

- H1 uses entry order only (per spec) — batched telemetry loses wall-clock
  ordering.
- Churn uses exact tree hashes; if a report_activity's workspace differs from
  the one checked, its tree is a no-op (empty) and contributes nothing.
- Thresholds are env-tunable (`VAULT_JACCARD_MIN`, `VAULT_CHURN_MIN_GROSS`,
  `VAULT_CHURN_MAX_EFF`) but not yet demo-tuned.

## Handoff to next phase

Phase 3 adds `TOOL_CALL_TRAP` (a `-30` penalty whose slot already exists in
`assess.go`), plus any additional heuristics, then demo-tuning of the
thresholds via the env vars.

## Metadata

- Commit: `6426b53` (`feat: phase 2 context-rot heuristics (H1 error loop, H2 churn, health aggregator)`)
- Tag: `phase-2-done`
- Branch: `cline/cbf4d`

## USER ACTION REQUIRED

- Reload/restart the Vault MCP server in Cline so the rebuilt binary
  (`/home/vivek/Documents/ClineAiHackathon/bin/vault`) and the settings
  (`disabled: false`, VAULT_ROOT, autoApprove) take effect. Everything else
  (build, verify, merge, tag, settings edit, log backup) was completed.


