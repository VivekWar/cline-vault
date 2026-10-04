# Phase 0 — Dev Environment Setup & Hardening

Status: **DONE** — 2026-10-04. No feature code; environment, tooling, and docs only.

## Goal

Stand up a clean, verified development environment for **Vault** (pure-Go 1.21+
MCP server over stdio, JSON-RPC 2.0, detects AI-agent context rot and writes a
handoff state file) such that `make verify` is green from day one and will stay
the definition of done for every later change.

## What was built

- `go.mod` — module `vault`, `go 1.21`.
- `Makefile` — `fmt-check`, `vet`, `test`, `build`, `verify` (ordered, stops at
  first failure, `.NOTPARALLEL`).
- `.gitignore` — `bin/`, `.vault/`, `coverage.out`, `*.out`, `*.test`.
- `.clinerules` — short hard-rule set (<40 lines).
- `ARCHITECTURE.md` — explanatory material, protocol constraints, planned
  layout, workflow, testing conventions.
- `testdata/README.md` — scaffold for fixture stderr logs and JSON-RPC
  transcripts.
- `.cline/memory-bank/progress.md` — progress log.
- This document.

## Decisions made

- **Fresh repo**: `git init` inside `/home/vivek/Documents/ClineAiHackathon`.
  The folder previously fell under the enclosing home-directory repo
  (`/home/vivek`, remote = `distributed-cache-cluster`); an isolated repo keeps
  Vault's history out of that project.
- **Module name**: `vault` (local, stdlib-only; rename later if published).
- **Skip logic**: `vet`/`test`/`build` SKIP **only** when the repo has zero
  `.go` files (each SKIP prints its reason). As soon as any `.go` file exists
  they run for real and fail the target on any error. This keeps `make verify`
  green on an empty module without ever masking a real failure.

## Problems encountered

- **Missing "existing" files**: the task assumed `.clinerules`,
  `ARCHITECTURE.md`, and the memory bank already existed; the folder was
  empty. Resolved by confirming with the user and creating them fresh.
- **Parent-repo commit scare**: `ClineAiHackathon` was inside the
  home-directory git repo (branch `main`, remote `origin` =
  `distributed-cache-cluster`). A raced `git commit` briefly executed against
  that parent repo; it exited with "no changes added to commit", so **nothing
  was staged or committed there**. Resolved by `git init`-ing an isolated repo
  and re-running the commit sequentially, verified by:
  `git -C /home/vivek diff --cached --name-only` (empty),
  `git -C /home/vivek ls-files Documents/` (0 lines), and no parent commit in
  any branch touching this project.

## Evidence — `make verify`

Empty module (current state, after Phase 0 hardening):

```
$ make verify
OK   fmt-check (nothing to format)
SKIP vet (reason: repo has zero .go files)
SKIP test (reason: repo has zero .go files)
SKIP build (reason: repo has zero .go files)
verify: all checks passed
$ echo $?
0
```

Proof that the gate runs for real once Go code exists (temporary
`tmp_probe.go` + deliberately failing `tmp_probe_test.go`, since deleted):

```
$ make verify
OK   fmt-check (nothing to format)
OK   vet
--- FAIL: TestAddProbe (0.00s)
    tmp_probe_test.go:10: deliberate failure for proof: Add(2,2) = 4, want 5
FAIL
FAIL	vault	0.004s
FAIL
make: *** [Makefile:28: test] Error 1
$ echo $?
2
```
