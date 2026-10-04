# Vault — Phase 4 Report

## Summary

Phase 4 implements the four Feature Expansion items:

1. **Feature B — Agent Directive.** `check_context_health`
   (`internal/state/state.go`) now returns a natural-language directive
   followed by the Health JSON instead of raw JSON alone. Status `healthy`
   yields `VERDICT: HEALTHY. Continue working.`; `degraded` and `critical`
   both yield `VERDICT: DEGRADED. Stop what you are doing. You are thrashing.
   Call create_handoff immediately and ask the user to start a new task.` The
   directive is separated from the JSON by a blank line. The `vault health`
   CLI keeps returning raw JSON (it calls `HealthJSON`, which is unchanged).
   The MCP tool description in `internal/mcp/tools.go` was updated to mention
   the directive.

2. **Feature C — HTML Report Command.** New package `internal/report`
   reads `.vault/activity.jsonl`, runs the same `heuristics.Assess`
   aggregation `check_context_health` uses, and writes a self-contained
   `.vault/report.html` (html/template + inline CSS, zero external assets).
   The page shows total actions, failing actions, health score/status, the
   list of triggered flags with evidence, a Net-vs-Gross churn table plus
   CSS bars, an error-loop table (pairwise similarities + shared tokens), and
   a recent-activity table (commands redacted). Bare `vault report` generates
   the report; see Deviations for how this coexists with the phase-3b
   telemetry form.

3. **Feature D — Redaction.** Pure `Redact(text string) string` in
   `internal/heuristics/redact.go` masks four secret classes with
   `[REDACTED]`: `sk-` API keys (20+ alphanumerics), bearer tokens
   (`Bearer <token>`, scheme word kept), PEM private key blocks
   (`-----BEGIN ... PRIVATE KEY-----` … `-----END ... PRIVATE KEY-----`),
   and `KEY=value` pairs whose key contains KEY/TOKEN/SECRET/PASSWORD
   (case-insensitive, value only). It is applied to `stderr` in
   `reportActivity` before the line reaches `activity.jsonl`, and to the
   entire handoff body in `buildHandoff`. Table-driven tests plus a
   `testdata/heuristics/redact_fixture.txt` fixture.

4. **Feature E — Compression Metric.** `buildHandoff`
   (`internal/state/handoff.go`) appends a footer to `handoff_state.md`:
   `---` + `Vault Compression Estimate: Condensed ~X tokens of activity
   history into ~Y tokens of handoff state. (Saved ~Z tokens).` Tokens are
   `characters/4`. X sums the `Command` and `Stderr` characters of the
   activity history; Y is the total characters of the final handoff string —
   computed to a fixed point, because the footer's own length is part of Y.
   Savings Z is clamped at 0.

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| healthy status → `VERDICT: HEALTHY. Continue working.` + JSON | PASS | `TestCheckContextHealthHealthyDirective`; manual stdio smoke test below |
| degraded/critical → `VERDICT: DEGRADED. Stop what you are doing…` + JSON | PASS | `TestCheckContextHealthLoopFlag` (degraded via RECURRING_ERROR_LOOP) |
| `vault health` CLI still raw JSON | PASS | `TestHealthCLI` unchanged and passing |
| `internal/report` generates self-contained `.vault/report.html` | PASS | `TestGenerateWithErrorLoop` (asserts no `http://`, `https://`, `<link`) |
| Report shows total actions, flags, churn net vs gross, error loops | PASS | same test + `TestGenerateEmpty` |
| `vault report` exists; default (no args) still stdio MCP server | PASS | `TestReportHTMLCLI`; `TestEndToEnd` (no-arg binary serves MCP) |
| Redaction of API keys, bearer tokens, private keys, KEY=value pairs | PASS | `TestRedact` (8 cases), `TestRedactFixture` |
| stderr redacted before reaching activity.jsonl | PASS | `TestReportActivityRedactsStderr`; manual stdio smoke test below |
| handoff_state.md output redacted | PASS | `TestHandoffRedactsSecrets` |
| Compression footer with X/Y/Z (chars/4) | PASS | `TestHandoffCompressionFooter` (2 cases), `TestCompressionFooterCountsOnlyCommandAndStderr` |
| `make verify` green | PASS | output below |

## Files built

- `internal/state/state.go` (236) — Feature B: verdict constants +
  `checkContextHealth` directive; Feature D: stderr redaction in
  `reportActivity`.
- `internal/state/handoff.go` (318) — Feature D: handoff body redaction;
  Feature E: `compressionFooter` + `savedTokens` appended in `buildHandoff`.
- `internal/state/health_test.go` (119) — updated loop test for the directive;
  new `TestCheckContextHealthHealthyDirective`; `textAfterDirective` helper.
- `internal/state/handoff_test.go` (146) — new: `TestHandoffRedactsSecrets`,
  `TestHandoffKeepsValidJSONArgs`, `TestHandoffCompressionFooter`,
  `TestCompressionFooterCountsOnlyCommandAndStderr`.
- `internal/state/state_test.go` (385) — new `TestReportActivityRedactsStderr`.
- `internal/heuristics/redact.go` (43) — pure `Redact` + 4 compiled regexes.
- `internal/heuristics/redact_test.go` (120) — table-driven + fixture tests.
- `testdata/heuristics/redact_fixture.txt` (11) — fixture log with all four
  secret classes.
- `internal/report/report.go` (292) — `Generate`, `buildPage`, page template.
- `internal/report/report_test.go` (130) — 5 tests.
- `cmd/vault/main.go` (133) — `vault report` dual form dispatch +
  `runReportHTML`.
- `cmd/vault/main_test.go` (217) — `TestReportHTMLCLI`.
## Design decisions

- **Directive split with `\n\n`.** The verdict line and JSON are separated by
  a blank line so an LLM can read the directive naturally and a machine can
  still slice the JSON off deterministically (`textAfterDirective` in tests).
  Rejected: putting the directive inside the JSON (changes the Health schema
  and breaks `vault health` consumers).
- **Verdict wording for `critical`.** The prompt gives one text for both
  `degraded` and `critical`; both use the DEGRADED wording as specified.
- **`vault report` dual form (see Deviations).** Because the phase-3b
  telemetry plugin invokes `vault report --root DIR '<json>'`, the subcommand
  dispatches on the presence of a positional argument: payload → telemetry
  route; no payload → HTML report. This keeps the plugin working byte-for-byte
  while honoring the phase-4 "add `vault report`" instruction.
- **Report reuses `heuristics.Assess`.** Flags/churn in report.html are the
  exact same numbers `check_context_health` reports — one source of truth, no
  reimplementation. Trees come from the log's `tree` fields (no fresh git
  snapshot), so the report is deterministic and works offline.
- **Redaction is applied last-in-write-path.** `reportActivity` redacts
  stderr before marshaling; `buildHandoff` redacts the fully assembled body,
  which also covers agent-supplied sections (goal, decisions, blocker) — the
  only ordering that guarantees secrets never reach disk even when they
  arrive via a section that isn't `stderr`.
- **Compression Y fixed point.** The footer reports the handoff's own length,
  so Y iterates until `len(body) + len(footer)` is stable (converges in a
  couple of iterations since only digit counts shift).
- **Redaction substring matching is spec-literal.** A key like `hockey=1`
  contains "key" and is masked; that false positive is the documented,
  accepted cost of never leaking credentials (noted in `redact.go`).
- **Savings clamped at 0.** Tiny sessions whose handoff is longer than the
  activity history report `(Saved ~0 tokens)` rather than a negative number.

## Deviations from the prompt

- **`vault report` already existed** (phase 3b telemetry route; the
  `vault-telemetry` plugin calls `vault report --root DIR '<json-payload>'`
  from its `afterTool` hook). It was preserved via positional-argument
  dispatch instead of being replaced. The plugin was NOT modified.
- **Feature D scope.** The prompt asked to apply redaction to the
  `handoff_state.md` output; implemented as redaction of the whole handoff
  body (covers stderr excerpts AND agent-supplied sections). Activity log
  redaction is stderr-only as specified (Command is also redacted in the HTML
  report's recent-activity table, beyond spec, because commands can carry
  `KEY=value` secrets).
- **Footer blank line.** A leading blank line is emitted before the `---`
  footer for valid Markdown (the file body already ends with a newline).
- **Tests were written alongside implementation, not strictly first** (same
  practice as phases 1–3: table-driven tests land in the same commit as the
  feature, and each commit was only made after `make verify` was green).
- **Critical status shares the DEGRADED wording** — the prompt only defines
  two directive texts.

## Problems encountered

- **`gofmt` failures on new test files** (twice): raw-string alignment in
  `redact_test.go` and `handoff_test.go` tables. Root cause: hand-written
  table alignment. Fix: `gofmt -w`; no test changes needed.
- **Unused imports after trimming a test** (`os`, `path/filepath` in
  `handoff_test.go`): removed.
- **Editor clobbered `var pageTmpl`** when splitting the large `report.go`
  into two edits (the second edit replaced the line that declared `pageTmpl`,
  leaving `pageHTML` unparsed and `html/template` unused). Fix: re-added
  `var pageTmpl = template.Must(...)` above the const. No failed commits —
  `make verify` caught it before commit.
## Test evidence

`go test ./... -v -count=1` summary (new Phase-4 tests):

```
=== RUN   TestReportHTMLCLI                     --- PASS
=== RUN   TestGenerateWithErrorLoop             --- PASS
=== RUN   TestGenerateEmpty                     --- PASS
=== RUN   TestBuildPageChurnBars                --- PASS
=== RUN   TestBuildPageRedactsCommands          --- PASS
=== RUN   TestRedact (8 subtests)               --- PASS
=== RUN   TestRedactFixture                     --- PASS
=== RUN   TestReportActivityRedactsStderr       --- PASS
=== RUN   TestHandoffRedactsSecrets             --- PASS
=== RUN   TestHandoffKeepsValidJSONArgs         --- PASS
=== RUN   TestHandoffCompressionFooter (2)      --- PASS
=== RUN   TestCompressionFooterCountsOnlyCommandAndStderr --- PASS
=== RUN   TestCheckContextHealthLoopFlag        --- PASS
=== RUN   TestCheckContextHealthHealthyDirective --- PASS
ok  vault/cmd/vault, vault/internal/heuristics, vault/internal/mcp,
    vault/internal/report, vault/internal/state
```

Full `make verify` output:

```
OK   fmt-check (nothing to format)
OK   vet
ok 	vault/cmd/vault	1.384s
ok 	vault/internal/heuristics	0.399s
ok 	vault/internal/mcp	0.022s
ok 	vault/internal/report	0.007s
ok 	vault/internal/state	0.204s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

Stdio smoke test (tools/call `report_activity` with a secret-bearing stderr,
then `check_context_health`):

```
$ printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"report_activity",...stderr with PASSWORD= and Bearer...}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"check_context_health","arguments":{}}}' \
  | VAULT_ROOT=$R ./bin/vault

-> id:2 result text: "VERDICT: HEALTHY. Continue working.\n\n{\"score\":100,...}"
-> activity.jsonl stderr: "export PASSWORD=[REDACTED]; curl -H Authorization: Bearer [REDACTED] x"
```

`vault report` smoke test: 3 seeded failing TEST entries, then bare
`vault report --root $R` → `wrote $R/.vault/report.html`, file starts with
`<!DOCTYPE html>` and contains the RECURRING_ERROR_LOOP flag.

## Known limitations

- Churn numbers in report.html come only from trees already recorded in
  activity.jsonl; no fresh snapshot is taken at report time (deliberate: the
  report must be reproducible).
- Redaction is regex-based and substring-tolerant for KEY/TOKEN/SECRET/
  PASSWORD names, so identifiers like `monkey=1` or `hockey=1` get masked.
  Non-`sk-` API-key formats (e.g. GitHub `ghp_...`) are not covered.
- Token estimates are `characters/4` (integer division), an approximation by
  design.
- The recent-activity table in report.html caps at the last 20 rows.

## Handoff to next phase

All four Phase-4 features are committed and green. Next phases (per the
original roadmap): whatever the next directive lists — the state package now
owns directive rendering, the report package is ready for extra sections, and
`Redact` can be extended with more patterns in `internal/heuristics/redact.go`.

## Metadata

- Commit: `161b4c8458bd9c7ad3cf87867e21bead6bb71f01` (HEAD at report time)
- Tag: `phase-4-done`
- Branch: `cline/d7a73`


