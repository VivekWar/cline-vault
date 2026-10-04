# Vault — Phase 3c Report

## Summary

Phase 3c optimizes and hardens the telemetry pipeline:

1. **I/O optimization — no snapshots for READ.** `reportActivity` in
   `internal/state/state.go` no longer runs `heuristics.Snapshot()` (a full
   `git add -A` + `git write-tree` on a temp index) for `READ` activities;
   `tree` stays `""`, which `compactTrees` already ignores. Only non-READ
   kinds pay the snapshot cost.
2. **Micro-oscillation detection.** `DetectOscillation` now takes the distinct
   snapshot count as a third parameter. It flags when
   `(gross > churnMinGross() || snapshots >= churnMinSnapshots()) && eff < churnMaxEff()` —
   i.e. the old 100-line threshold OR 10+ snapshots of thrashing, so repeatedly
   flipping a single line (e.g. `>` ↔ `>=`) 12 times is detected even though
   line churn stays tiny. `Churn` now also returns the compacted snapshot
   count, and `Assess` threads it through. The snapshot threshold is a named
   constant (`defaultChurnMinSnapshots = 10`), env-overridable via
   `VAULT_CHURN_MIN_SNAPSHOTS`, consistent with the other thresholds.

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| READ activities skip the git snapshot | PASS | `TestReportActivitySkipsSnapshotForRead` (READ line has no `tree`, EDIT line does) |
| Non-READ kinds still snapshot | PASS | same test |
| `DetectOscillation(net, gross, snapshots)` signature | PASS | `churn.go` |
| Micro-oscillation (10+ snapshots, tiny gross) flags | PASS | `TestDetectOscillation/micro_oscillation_*`, `TestChurnIntegration/micro_oscillation_flags`, `TestAssess/micro_oscillation_only` |
| Old line-threshold behavior preserved | PASS | existing oscillation cases still pass |
| `make verify` green | PASS | output below |

## Files built

- `internal/heuristics/churn.go` (208) — `Churn` returns `(net, gross, snapshots)`; `DetectOscillation` gains the `snapshots` gate.
- `internal/heuristics/assess.go` (143) — `defaultChurnMinSnapshots` const + `churnMinSnapshots()`/`envInt`; `Assess` passes snapshots through.
- `internal/state/state.go` (217) — READ-kind snapshot skip in `reportActivity`.
- `internal/heuristics/heuristics_test.go` (379) — `TestDetectOscillation` table extended (3 micro cases), `TestAssess` micro subtest.
- `internal/heuristics/churn_integration_test.go` (169) — `Churn` 3-value call sites, `TestChurnIntegration/micro_oscillation_flags` (single-line flip ×12, gross 24 → flags).
- `internal/state/state_test.go` (367) — `TestReportActivitySkipsSnapshotForRead`.

## Design decisions

- **Snapshot gate reuses `compactTrees` semantics.** The snapshots count is the
  length of the compacted tree list (empty trees and consecutive duplicates
  removed), so repeated identical snapshots cannot fake 10+ "oscillations".
- **Snapshots threshold is env-tunable** (`VAULT_CHURN_MIN_SNAPSHOTS`, default
  10) alongside the existing `VAULT_*` knobs, since the demo will need tuning.
- **README skip keeps `tree` empty rather than duplicating the last tree**:
  copying the previous tree would make `compactTrees` collapse it anyway and
  would add noise to the activity log.

## Deviations from the prompt

- None. The `snapshots >= 10` gate was implemented as the named constant
  `churnMinSnapshots()` (default 10) instead of a literal, for consistency with
  the other overridable thresholds.

## Problems encountered

- None.

## Test evidence

- `go test ./internal/... -v -run 'TestDetectOscillation|TestChurnIntegration|TestAssess|TestReportActivitySkipsSnapshotForRead'`:
  all subtests pass, including 3 new micro-oscillation unit cases, the
  integration micro-oscillation case (gross 24, snapshots 13 → flag), the
  `TestAssess/micro_oscillation_only` case (score 65, degraded), and the READ
  snapshot-skip test.

Full `make verify` output:

```
OK   fmt-check (nothing to format)
OK   vet
ok  	vault/cmd/vault	0.783s
ok  	vault/internal/heuristics	0.394s
ok  	vault/internal/mcp	0.014s
ok  	vault/internal/state	0.138s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

- None required beyond the automated tests.

## Known limitations

- The snapshots gate can flag slow-but-genuine progress if an agent takes 10+
  snapshots each with tiny net change but small efficiency; threshold is
  env-tunable for the demo.

## Handoff to next phase

Phase 4: verify live telemetry and demo-tune all thresholds
(`VAULT_JACCARD_MIN`, `VAULT_CHURN_MIN_GROSS`, `VAULT_CHURN_MAX_EFF`,
`VAULT_CHURN_MIN_SNAPSHOTS`) against real telemetry.

## Metadata

- Commit: `a19f1ba` (`feat: phase 3c telemetry optimization and hardening (READ snapshot skip, micro-oscillation)`)
- Tag: `phase-3c-done`
- Branch: `cline/cbf4d`

## USER ACTION REQUIRED

- Restart Cline / start a new session so the rebuilt binary and plugin are in
  effect. Everything else (tests, verify, commit, merge, rebuild, settings
  check, log backup) was completed.
