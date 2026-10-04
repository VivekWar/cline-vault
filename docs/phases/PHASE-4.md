# Vault — Phase 4 Report (revised)

## Summary

Phase 4 implemented the Feature Expansion items that remain in the
codebase:

1. **Feature B — Agent Directive.** `check_context_health`
   (`internal/state/state.go`) returns a natural-language directive
   followed by the Health JSON instead of raw JSON alone. Status `healthy`
   yields `VERDICT: HEALTHY. Continue working.`; `degraded` and `critical`
   both yield `VERDICT: DEGRADED. Stop what you are doing. You are
   thrashing. Call create_handoff immediately and ask the user to start a
   new task.` The `vault health` CLI keeps returning raw JSON.

2. **Feature D — Redaction.** Pure `Redact(text string) string` in
   `internal/heuristics/redact.go` masks four secret classes with
   `[REDACTED]`: `sk-` API keys (20+ alphanumerics), bearer tokens, PEM
   private key blocks, and `KEY=value` pairs whose key contains
   KEY/TOKEN/SECRET/PASSWORD (case-insensitive, value only). Applied to
   `stderr` in `reportActivity` before the log write and to the whole
   handoff body in `buildHandoff`.

3. **Feature E — Compression Metric.** `buildHandoff` appends a footer to
   `handoff_state.md`: `Vault Compression Estimate: Condensed ~X tokens of
   activity history into ~Y tokens of handoff state. (Saved ~Z tokens).`
   Tokens are characters/4; X sums Command+Stderr characters; Y is the
   final handoff length (computed to a fixed point); savings clamped at 0.

**Removed features:** Feature C (the static HTML report command) and the
Phase 4c real-time HTMX dashboard (and its 4c.1 UI revision) were removed
at the user's request. There is no web UI code left: the `internal/report`
package, the `vault serve` subcommand, and the `vault report` HTML form are
gone. `vault report '<json>'` remains, purely as the telemetry route the
vault-telemetry plugin calls to record activity.

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| healthy status → `VERDICT: HEALTHY. Continue working.` + JSON | PASS | `TestCheckContextHealthHealthyDirective` |
| degraded/critical → `VERDICT: DEGRADED. Stop what you are doing…` + JSON | PASS | `TestCheckContextHealthLoopFlag` |
| `vault health` CLI still raw JSON | PASS | `TestHealthCLI` |
| Redaction of API keys, bearer tokens, private keys, KEY=value pairs | PASS | `TestRedact` (8 cases), `TestRedactFixture` |
| stderr redacted before reaching activity.jsonl | PASS | `TestReportActivityRedactsStderr` |
| handoff_state.md output redacted | PASS | `TestHandoffRedactsSecrets` |
| Compression footer with X/Y/Z (chars/4) | PASS | `TestHandoffCompressionFooter`, `TestCompressionFooterCountsOnlyCommandAndStderr` |
| No web UI code remains (report package, serve subcommand, HTMX) | PASS | `internal/` contains only heuristics/mcp/state; `make verify` green |
| `vault report '<json>'` telemetry route intact | PASS | `TestReportCLI`, plugin invocation path |
| `make verify` green | PASS | output below |

## Files (current state)

- `internal/state/state.go` (236) — verdict constants, directive in
  `checkContextHealth`, stderr redaction in `reportActivity`.
- `internal/state/handoff.go` (318) — handoff redaction, compression footer.
- `internal/state/health_test.go` (119), `handoff_test.go` (146),
  `state_test.go` (385) — directive/redaction/footer tests.
- `internal/heuristics/redact.go` (43) + `redact_test.go` (120) + fixture
  `testdata/heuristics/redact_fixture.txt` (11).
- `cmd/vault/main.go` — health + report (telemetry-only) subcommands.
- `cmd/vault/main_test.go` — e2e, health, report CLI tests.
- `internal/mcp/tools.go` (108) — `check_context_health` description.

## Design decisions

- **Directive split with `\n\n`** so an LLM reads the verdict naturally and
  a machine can still slice the JSON off deterministically.
- **Redaction applied last-in-write-path** (`reportActivity` redacts stderr
  before marshaling; `buildHandoff` redacts the assembled body) so secrets
  never reach disk via any section.
- **Compression Y fixed point** because the footer is part of the handoff
  string it measures.
- **Redaction substring matching is spec-literal** (e.g. `hockey=1` is
  masked); accepted false-positive trade-off, documented in `redact.go`.
## Deviations from the prompt

- **Feature C and the Phase 4c/4c.1 dashboard were removed on user request.**
  The original prompt asked for an HTML report command and (in 4c) a
  real-time HTMX dashboard; both were built, then deleted in a follow-up
  revision. The telemetry form of `vault report '<json>'` was retained
  because the vault-telemetry plugin depends on it.
- **Critical status shares the DEGRADED wording** — the prompt only defines
  two directive texts.
- **Tests were written alongside implementation** (each commit only landed
  after `make verify` was green).

## Problems encountered

- `gofmt` alignment on new test tables; unused imports; a large-file editor
  split that clobbered a template declaration during the (now-removed)
  report work. All caught by `make verify` before commit.
- No failed commits reached the branch.

## Test evidence

`go test ./... -v -count=1` — all packages pass, including the Phase-4
suites: `TestCheckContextHealthLoopFlag`,
`TestCheckContextHealthHealthyDirective`, `TestRedact` (8 cases),
`TestRedactFixture`, `TestReportActivityRedactsStderr`,
`TestHandoffRedactsSecrets`, `TestHandoffCompressionFooter` (2 cases),
`TestCompressionFooterCountsOnlyCommandAndStderr`, `TestReportCLI`.

Full `make verify` output:

```
OK   fmt-check (nothing to format)
OK   vet
ok 	vault/cmd/vault	0.739s
ok 	vault/internal/heuristics	0.362s
ok 	vault/internal/mcp	0.024s
ok 	vault/internal/state	0.168s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

Stdio smoke test: `report_activity` with a secret-bearing stderr persists
`PASSWORD=[REDACTED]` / `Bearer [REDACTED]`; `check_context_health` returns
`VERDICT: HEALTHY. Continue working.` followed by the Health JSON.

## Known limitations

- Redaction is regex-based; non-`sk-` key formats (e.g. `ghp_…`) are not
  covered; substring false positives (e.g. `monkey=1`) are masked.
- Token estimates are `characters/4` (integer division), by design.

## Handoff to next phase

The codebase is back to a clean CLI + MCP shape: telemetry recording,
health directive, redaction, and handoff compression. No web UI remains.

## Metadata

- Branch: `cline/d7a73` (main diverged earlier via a parallel session; see
  phase 4c notes — not fast-forwarded).
- Tag: `phase-4-done` re-pointed to the removal commit; `phase-4c-done`
  deleted.

