# Vault — Phase 5 Report

## Summary

Phase 5 builds the **demo trap**: a deliberately broken Go module at
`demo-trap/` that is designed to trap an LLM in an error loop, so Vault can
demonstrate catching it live in front of the judges.

- `demo-trap/` is its own Go module (`go mod init demo-trap`).
- It contains one source file (`store.go`) and one test file (`trap_test.go`).
- The bug is a **concurrent map read/write** in a realistic in-memory TTL
  "session store" used by an HTTP layer. The store has a background `janitor`
  goroutine, a `Get` that **mutates** the map (lazy eviction), and a
  `sync.RWMutex` field that exists but is never wired up.
- `go test .` dies with `fatal error: concurrent map iteration and map write`
  (or `concurrent map read and map write` / `concurrent map writes`) on
  **10/10** repeated runs.
- The trap has **multiple layers** (verified empirically, see Design
  decisions): locking only some methods still panics; wrapping the janitor's
  whole loop in a held lock deadlocks; using `RLock` for `Get` still panics
  because `Get` deletes.
- `demo-trap/` is added to the repository `.gitignore`; `make verify` stays
  green.

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| `demo-trap/` is a new Go module `demo-trap` | PASS | `demo-trap/go.mod` (`module demo-trap`, `go 1.24.0`) |
| A Go source file + `trap_test.go` that fails on a subtle bug | PASS | `demo-trap/store.go` (104 lines) + `demo-trap/trap_test.go` (56 lines); `go test .` → `fatal error: concurrent map iteration and map write` |
| Bug is one an LLM typically loops on | PASS | concurrency bug; naive fixes A/B/C/D all still fail (table below); only the correct fix (E) passes |
| `demo-trap/` ignored in the main `.gitignore` | PASS | `git check-ignore -v demo-trap` → `.gitignore:14:demo-trap/` |
| `Makefile`/`test.sh` in `demo-trap/` that just runs `go test .` | PASS | `demo-trap/Makefile` (`test:` → `go test .`), `demo-trap/test.sh` (`exit=1`, expected) |
| `make verify` stays green (trap does not break it) | PASS | full output below; `demo-trap` is a nested module excluded from `./...`, and its files are gofmt-clean so `fmt-check` passes |
| Phase checklist executed with N = 5 | PASS | this document + `phase-5-done` tag + FF-merge + binary/MCP/log steps |

## Files built

Line counts from `wc -l` (in `demo-trap/` unless noted):

- `demo-trap/go.mod` (3) — new module `demo-trap`.
- `demo-trap/store.go` (104) — intentionally broken in-memory session cache:
  `Store`, `NewStore`, `Close`, `Set`, `Get` (deletes on read), `Len`, and the
  background `janitor`.
- `demo-trap/trap_test.go` (56) — `TestConcurrentSessionAccess`: 16 workers
  (writers / readers / stats) hammering the store for a 250 ms window while
  the janitor sweeps.
- `demo-trap/Makefile` (11) — `test:` target runs `go test .`.
- `demo-trap/test.sh` (5) — `cd` to its own dir and run `go test .`.
- `.gitignore` (main repo) — added `demo-trap/`.

## Design decisions

- **Concurrent map read/write as the trap.** The prompt offered a concurrent
  map bug or a subtle slice-bounds bug. The map bug was chosen because it is
  a real-world class of failure, it fails loudly (`fatal error`), and it has
  several plausible-but-wrong fixes — exactly what makes an agent loop.
- **Realistic framing.** The store looks like a normal session cache: TTLs,
  lazy eviction on read, a background janitor, a `Len` stats endpoint. Nothing
  screams "toy".
- **Layered trap (empirically measured).** Fixes were tried in scratch copies
  and timed:

  | Variant | Fix attempt | Result |
  |---|---|---|
  | original | no locking | **FAIL 10/10** — `concurrent map iteration/read and map write` |
  | A | locks `Set`/`Get`/`Len`, forgets the janitor | **FAIL** — janitor still races |
  | B / C | wraps the janitor's whole loop in `Lock()` | **HANG** (deadlock, exit 124) |
  | D | `RLock` in `Get` ("it's a read"), correct janitor lock | **FAIL** — `Get` deletes under `RLock`, so `Len` iteration races a write |
  | E | `Lock` in `Get`, `RLock` in `Len`, scoped `Lock` in the janitor | **PASS** (0.25 s) |

  This is the intended demo behaviour: several confident fixes still loop.
- **Unused `sync.RWMutex` field.** A leftover from a "started but never
  finished" refactor. It nudges the agent toward `RWMutex` — and toward the
  wrong (`RLock` in `Get`) fix that keeps the loop going.
- **Deadlock as a bonus failure mode.** Locking the janitor's entire loop is a
  common mistake and produces a hang rather than a panic — a *different*
  symptom that keeps the agent guessing.
- **Rejected: a flaky/`-race`-only trap.** Relying on `-race` would make the
  demo depend on a flag the prompt's `go test .` does not use. The runtime's
  built-in map-write detection fires deterministically here.
- **Rejected: a slice-bounds panic.** Deterministic, but usually a one-line
  off-by-one fix; it loops far less than a concurrency bug.

## Deviations from the prompt

- The prompt said "write a Go file and a test file `trap_test.go`". Implemented
  as a single source file (`store.go`) plus `trap_test.go`.
- **Tests were written alongside the implementation, not strictly first** — the
  same practice as phases 1–4. (The "test" here is the trap itself, so it is
  inseparable from the code it traps.)
- Added **both** a `Makefile` and a `test.sh` (the prompt said "or"); both just
  run `go test .`.
- `demo-trap/go.mod` is `go 1.24.0` (produced by `go mod init` under the
  installed toolchain). The `.clinerules` "keep go.mod at 1.21" rule applies to
  the Vault module, not this separate demo module. Vault's own `go.mod` is
  unchanged at `go 1.21`.

## Problems encountered

- **`fmt-check` walks ignored files.** `.gitignore` alone does *not* stop
  `gofmt -l .` (it walks the filesystem) or `find . -name '*.go'` (the
  Makefile's probe). Root cause: Go tooling ignores `.gitignore`. Fix: the
  `demo-trap` Go files are kept gofmt-clean, and `demo-trap` is a nested module
  so `go vet ./...` / `go test ./...` skip it. Verified: `make verify` green
  with `demo-trap/` present.
- **Scratch-fix experiments B/C hung** (exit 124). Root cause: the *fix patch
  itself* held `s.mu.Lock()` across the janitor's entire `for` loop, blocking
  every other goroutine. This was a mistake in the experiment, but it turned
  out to be a genuine, useful failure mode of the trap (documented above as
  variant B/C). Re-run with a correctly scoped janitor lock (D/E) isolated the
  `RLock`-in-`Get` race.

## Test evidence

`go test ./... -v -count=1` (Vault module) summary:

```
100 x --- PASS
  0 x --- FAIL
ok  vault/cmd/vault, vault/internal/heuristics, vault/internal/mcp,
    vault/internal/report, vault/internal/state
```

Trap evidence (`demo-trap/`):

```
$ go test .
fatal error: concurrent map iteration and map write
...
demo-trap.(*Store).Len(...)  store.go:77
demo-trap.TestConcurrentSessionAccess.func1(...)  trap_test.go:49
exit=1

$ for i in $(seq 1 10); do go test .; done   # (exit codes)
RESULT: failed 10/10 runs

$ ./test.sh
fatal error: concurrent map iteration and map write
test.sh exit=1
```

Full `make verify` output (repository root):

```
OK   fmt-check (nothing to format)
OK   vet
ok  	vault/cmd/vault	1.397s
ok  	vault/internal/heuristics	0.396s
ok  	vault/internal/mcp	0.017s
ok  	vault/internal/report	0.006s
ok  	vault/internal/state	0.184s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

- `git check-ignore -v demo-trap demo-trap/store.go` ->
  `.gitignore:14:demo-trap/` (both ignored).
- `git status --porcelain --untracked-files=all` does not list `demo-trap/`.
- Deployed binary `tools/list` returns
  `report_activity, check_context_health, create_handoff, read_handoff`.

User-reported issues: none required.

## Known limitations

- The trap's failure is a **runtime `fatal error`**, which aborts the whole
  test binary; other tests in the same package would not run. That is fine
  here (there is only one test) and is realistic for a `concurrent map` bug.
- `demo-trap/` is **git-ignored**, so it is intentionally not committed and
  will not appear in a fresh clone; it lives on disk in this worktree (and a
  copy is placed in the deployed checkout for the demo).
- The demo relies on the Go runtime's built-in concurrent-map detection
  (Go 1.24 Swiss maps). Behaviour is deterministic on this toolchain but is
  not a general-purpose race detector.

## Handoff to next phase

- The trap is ready to drive a live demo: run `demo-trap/test.sh`, watch the
  agent loop on the panic, and have `check_context_health` return
  `VERDICT: DEGRADED`.
- If a stronger/longer loop is wanted, add a second failing assertion to
  `trap_test.go` so a race fix alone is not enough.
- All Phase-5 artifacts are committed and green; no Vault source code changed
  in this phase.

## Metadata

- Commit: `afc1588` (`docs: phase 5 demo trap report`; tagged
  `phase-5-done`)
- Tag: `phase-5-done`
- Branch: `cline/01800` (fast-forwarded into `main`)
