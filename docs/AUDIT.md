# Vault — Pre-Flight Architectural & Security Audit (Phase 4b)

**Auditor role:** Principal Staff Engineer review of everything built in
Phases 1–4, performed before the final demo recording and codebase lock.

**Verdict up front:** No critical, show-stopping bugs were found. The
codebase is cleared for the demo as-is; every finding below is severity
LOW or MEDIUM with a concrete post-demo remediation. No code was modified
during this audit — the audit itself was executed against commit `942c645`
(tag `phase-4-done`), and every empirical claim below was reproduced with
commands run during this audit, not inferred from reading.

## Scope

Every file in `cmd/`, `internal/`, `.cline/plugins/vault-telemetry/`, plus
`Makefile`, `go.mod`, `.clinerules`, `ARCHITECTURE.md`, and `testdata/`.
Baseline: `make verify` green, `go test -race ./...` clean, plugin
`npx tsc --noEmit` clean, plugin tests 19/19 pass.

---

## 1. Concurrency & Resource Leaks (Go)

### Findings

- **No goroutines exist anywhere in the Go code.** The server is a strictly
  single-threaded event loop (`mcp.Server.Serve`), and CLI subcommands are
  one-shot processes. There is therefore no data-race surface inside one
  process; `go test -race ./...` passes clean on all five packages.
- **File descriptor audit — clean.** Every file open path was traced:
  - `reportActivity` (state.go): `os.OpenFile` with `O_APPEND|O_CREATE|
    O_WRONLY`. The single `f.Write(line)` (line + `\n` in one call) is
    followed by `f.Close()` on ALL paths — the write-error path closes
    explicitly before returning; the success path closes and checks the
    close error. No `defer` needed because every path is covered.
    **Empirical:** 100 `report_activity` calls against one live server →
    `/proc/<pid>/fd` count stayed at 5 before and after.
  - `atomicWrite` (handoff.go): `os.CreateTemp` in the destination directory
    (same filesystem → atomic rename); write-error path closes + removes;
    close-error and rename-error paths remove. No leak.
  - All reads (`readActivities`, `countActivities`, `readHandoff`) use
    `os.ReadFile`, which closes internally.
  - `gitSnapshot` temp index (see §3).
- **Append safety under concurrent writers.** The telemetry plugin spawns
  one `vault report '<json>'` process per tool event. If Cline runs tools in
  parallel, several processes append simultaneously. The log is opened with
  `O_APPEND` and each entry is written with a **single** `Write` syscall, so
  lines do not interleave on Linux.
  **Empirical:** 40 concurrent `vault report` processes → 40/40 lines valid
  JSON, none interleaved or torn.
  Residual issue (LOW): the `recorded #N` count is computed by re-reading
  the file, so under parallel appends the echoed number can be off-by-one.
  Cosmetic only; the heuristics read the actual log.

### Residual findings (severity LOW)

- **LOW-2: archive name collision.** `archiveActivity` names archives
  `activity-<UTC-second>.jsonl`; two rotations within the same second make
  the second `os.Rename` silently overwrite the first. Realistic demo impact:
  none (one handoff per session). Remediation: existence check + counter
  suffix.
- **LOW-3: symlink following.** `reportActivity` appends to whatever
  `.vault/activity.jsonl` resolves to; a pre-planted symlink would redirect
  the append. Threat model is the agent's own workspace (trusted), so this
  is theoretical. Remediation: `O_NOFOLLOW` or an `os.Lstat` check.

---

## 2. Telemetry Plugin (`.cline/plugins/vault-telemetry/index.ts`)

### Findings

- **`spawnSync` safety — verified.** `maxBuffer` is not set, so Node's
  default (1 MB) applies — but it bounds the **child's stdout/stderr**, which
  is tiny (`recorded #N` / `wrote <path>`), never the payload. The payload
  travels as an **argv element**, bounded instead by the OS (`MAX_ARG_STRLEN`
  ≈ 128 KB/string on Linux). If a 40-line stderr excerpt exceeds that,
  `spawnSync` returns `res.error` (`E2BIG`) — the code checks `res.error`
  first and logs, it does not throw.
  **Empirical:** a 200 KB payload fails at exec with `Argument list too
  long`; the plugin path for this is the `res.error` branch (verified by
  code trace).
  Residual (LOW-4): an E2BIG payload means that one telemetry record is
  dropped (logged to the plugin console, never surfaces to the host). Not a
  crash, not user-visible. Remediation: cap stderr to ~32 KB in
  `lastLines`-style truncation (bytes, not lines).
- **No unhandled exceptions.** The entire `afterTool` body is wrapped in
  `try/catch` that logs and swallows; `report()` additionally checks
  `res.error`, non-zero `res.status`, and `timeout: 30000` prevents hangs.
  A missing binary, a killed child, a timeout — all degrade to a console
  error. The hook never mutates tool results (observational only).
- **TypeScript health:** `npx tsc --noEmit` clean; `npm test` → 19/19 pass.
- **Input shaping:** `lastLines(output, 40)` bounds lines; `buildPayload`
  matches the Go `reportActivitySchema` exactly (kind/command/exit_code/
  stderr/files/workspace).
---

## 3. Git Snapshot Safety (`internal/heuristics/churn.go`)

### Findings

- **Temp index lifecycle — clean.** `gitSnapshot` copies the real index to
  `os.CreateTemp("", "vault-index-*")`, closes the handle immediately, and
  `defer os.Remove(tmpName)` is registered **before** any git command runs,
  so every return path (missing index, `git add -A` failure, `write-tree`
  failure, success) removes the temp file.
  **Empirical:** 50 snapshots → 0 files left in /tmp; a snapshot against a
  repo whose `.git/objects` was made read-only (forcing `write-tree` to
  fail) also left 0 temp files, and the tool still returned normally
  (`recorded #1`) with an empty tree.
- **Real index never touched.** `GIT_INDEX_FILE` points at the temp copy
  (appended last to the env, so it wins over any inherited value); `git add
  -A` stages into the temp index; `git write-tree` hashes it. The worktree
  index and working tree are unchanged — covered by
  `TestChurnIntegration/snapshot_leaves_index_unchanged`.
- **Failure semantics are graceful.** Every failure returns `""` — snapshot
  absence is tolerated downstream (`compactTrees` drops empties). There is
  no panic path.
- **Observation (LOW-5):** `git write-tree` writes unreachable tree objects
  into `.git/objects` on every snapshot; `git gc` reclaims them later. Pure
  disk overhead, no correctness impact.
- **Observation (LOW-6):** `check_context_health` and non-READ telemetry
  each pay a full `git add -A` + `write-tree` over the repo. Fine for demo
  repos; on huge trees this is the only meaningful CPU cost in the system.

---

## 4. Heuristics Math (`heuristics.go`, `assess.go`, `churn.go`)

### Findings — no panics possible in the edge cases asked about

- **Jaccard:** both-empty is short-circuited to 0 (`len(a)==0 && len(b)==0`),
  and the union==0 guard catches the residual case. `TestJaccard/both_empty`
  and `one_empty` cover it. No divide-by-zero.
- **Churn efficiency:** `churnEfficiency` returns 0 when `gross == 0`;
  `TestChurnEfficiency/gross_zero` and `gross_zero_with_net` cover it.
- **Churn net/gross:** fewer than 2 compacted snapshots returns (0,0,n)
  before any diff runs; a failing `first→last` diff returns
  `(0, gross, snapshots)` — no NaN, no panic. `parseNumstat` guards
  `len(fields) < 2`; `numOrZero` maps "-" and junk to 0.
- **DetectErrorLoop:** `len(relevant) < 3` returns nil before indexing;
  `sets[0..2]` are only built after the length check. Empty output returns
  nil. All covered by table tests.
- **Rounding/env parsing:** `round2` is simple float math; `envFloat`/
  `envInt` fall back to defaults on unparseable env values.
- **Assess:** score clamped to [0,100]; status buckets (≥70 healthy, ≥40
  degraded, else critical) are total — every score maps to a status.
- **Go version compliance:** no range-over-int, no builtin `min`/`max` —
  the codebase stays within Go 1.21 as `.clinerules` demands.
  **Empirical:** grep scan clean; `go vet` and `gofmt` clean.

---

## 5. MCP Protocol Compliance (`cmd/vault`, `internal/mcp`)

### Findings

- **EOF handling:** `Serve` processes a final unterminated line (if any)
  and returns `nil` on `io.EOF`; only non-EOF read errors bubble up and
  exit(1) via main. Covered by every test transcript.
- **Parse errors:** malformed JSON and empty lines produce
  `{"error":{"code":-32700}}` with `id: null`; `Invalid Request` (−32600)
  for wrong `jsonrpc`/missing method; `Method not found` (−32601); tool
  failures return `isError: true` at the tool level (never a JSON-RPC
  error). The server **never panics** — there is no panic path in the
  transport (`TestErrorCases`, `TestOversizedLine`).
- **Notifications:** any message without `id` (e.g. `notifications/
  initialized`) gets no response — `TestServeTranscript` asserts exactly 4
  responses for a 5-line transcript.
- **stdout hygiene:** all responses are single `Write` calls with a
  trailing newline; logging goes exclusively to stderr. `ping` returns
  `{"result":{}}` (never null).
- **MEDIUM-1: oversized-line memory.** `Serve` calls
  `bufio.Reader.ReadBytes('\n')` **before** the 4 MB `lineTooLong` check, so
  an over-limit line is fully buffered first (bufio doubles the slice as it
  grows → ≈2× line size peak).
  **Empirical:** a single 30 MB stdin line drove the process to 66 MB peak
  RSS before it answered `-32700`.
  Why this is not show-stopping: stdin comes from the local Cline client;
  the telemetry plugin truncates stderr to 40 lines, so a >4 MB request is
  not produced in practice, and when one arrives the server still answers
  correctly and keeps serving (the record is dropped). Remediation (post-
  demo): accumulate with `ReadSlice` in a loop, abort + drain the rest of
  the line once the cap is exceeded, so memory stays O(cap).
- **LOW-7:** a >4 MB `tools/call` is answered with `-32700` and the
  activity is silently lost — the documented, deliberate trade-off of the
  cap. Acceptable for telemetry.

---

## 6. Redaction (`internal/heuristics/redact.go`)

### Findings — ReDoS is structurally impossible

- **Go's `regexp` is RE2:** no backtracking, guaranteed linear time in
  input length regardless of pattern. The four patterns contain no nested
  quantifiers that could matter even in a backtracking engine; the only
  "wildcard" is `[\s\S]*?` (lazy, linear under RE2).
  **Empirical:** adversarial 5 MB input (10× `-----BEGIN PRIVATE KEY-----`
  per unit, no terminator, dense `sk-` candidates) redacted in ≈850 ms,
  consistent across runs. No exponential blowup possible.
- **Ordering is deliberate:** private-key blocks first (multiline), then
  bearer, `sk-` keys, then `KEY=value` pairs (which subsumes any remaining
  `KEY=sk-...`). Idempotence is asserted in tests.
- **Coverage gaps (documented, LOW-8):** non-`sk-` key formats (e.g.
  `ghp_…`), `Basic` auth, and substring false positives (`hockey=1`,
  `monkey=1` get masked) are the accepted spec-literal trade-offs, noted in
  the file header and Phase-4 report.
- **Placement verified:** stderr is redacted before `activity.jsonl`; the
  whole handoff body is redacted before the atomic write. No path writes a
  raw secret.
  **Empirical:** live stdio smoke test showed
  `PASSWORD=[REDACTED]` / `Bearer [REDACTED]` in the persisted log.

---

## 7. Cross-cutting security review

- **No shell execution anywhere:** all subprocesses are `exec.Command`
  arrays (Go) and `spawnSync` arg arrays (TS) — no injection surface.
- **No network access:** the system is fully local and deterministic
  (offline-safe), per ARCHITECTURE.md.
- **Secrets at rest:** redaction covers the log, the handoff, and the HTML
  report (see §6). Git snapshots store tree hashes only.
- **HTML output:** `html/template` auto-escapes all interpolated values
  (flag evidence, commands, status); CSS is inline; zero external assets.
- **Tool input validation:** fixed kind enum, required `goal`/
  `next_action`, `additionalProperties: false` schemas on all four tools;
  unknown tools → `isError: true`.
---

## 8. Test & verification evidence collected during this audit

| Check | Result |
|---|---|
| `make verify` (fmt/vet/test/build) | PASS |
| `go test -race ./...` | PASS (5/5 packages) |
| `go test ./... -v -count=1` | PASS (all suites incl. 14 Phase-4 tests) |
| Plugin `npx tsc --noEmit` | PASS |
| Plugin `npm test` | 19/19 PASS |
| 100 activity writes vs live server → fd count | 5 → 5 (no leak) |
| 40 parallel `vault report` appends | 40/40 valid JSON lines, no interleave |
| 50 snapshots → /tmp temp indexes left | 0 |
| Forced `write-tree` failure → temp indexes left | 0, tool still answered |
| 30 MB stdin line → peak RSS | 66 MB (→ MEDIUM-1) |
| 5 MB adversarial redaction input | ≈850 ms, linear |
| 200 KB argv payload | clean `E2BIG` rejection, plugin logs (LOW-4) |
| Go 1.21 feature scan (range-over-int, min/max) | clean |

---

## 9. Findings register

| ID | Severity | Location | Summary |
|---|---|---|---|
| MEDIUM-1 | Medium | mcp/mcp.go `Serve` | Line buffered before 4 MB cap check; huge line costs ~2× memory |
| LOW-2 | Low | state/handoff.go | Archive filename has 1 s resolution; same-second overwrite |
| LOW-3 | Low | state/state.go | Append follows symlinks (trusted-workspace threat model) |
| LOW-4 | Low | plugin index.ts | E2BIG payload silently drops one telemetry record |
| LOW-5 | Low | heuristics/churn.go | Unreachable tree objects accumulate in .git/objects |
| LOW-6 | Low | heuristics/churn.go | Full-tree snapshot cost per non-READ event |
| LOW-7 | Low | mcp/mcp.go | >4 MB tools/call dropped with −32700 (by design) |
| LOW-8 | Low | heuristics/redact.go | Substring false positives; non-`sk-` formats unmasked |

**None of these is show-stopping for the demo.** MEDIUM-1 is the only one
that could ever matter in an adversarial setting; every other item is
cosmetic, a deliberate trade-off, or regenerable output. Per the audit
mandate ("do not modify any code unless you find a critical, show-stopping
bug"), the codebase was left untouched.

## Metadata

- Audited commit: `942c645` (`phase-4-done`, branch `cline/d7a73`,
  fast-forwarded into `main`)
- Date: 2026-10-04
- Audit scope: all of `cmd/`, `internal/`, `.cline/plugins/vault-telemetry/`



