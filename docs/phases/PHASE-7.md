# Vault — Phase 7 Report

## Summary

Phase 7 is the submission-documentation pass: docs only (plus the
git-ignored demo-trap), no Go behavior changes.

1. **README.md rewritten for copy-paste setup** — numbered quickstart where
   every step is a runnable code block (prereqs with one-line checks,
   clone, build + smoke test with expected output, MCP registration both
   ways — `make install` and a manual JSON block with real settings paths
   for Linux/macOS/Windows and clearly marked placeholders — plugin install
   with the SDK/CLI/Kanban host caveat, the agent-rule text block, restart
   + first prompt and what to expect), plus verify-it-works commands,
   uninstall, and a troubleshooting table.
2. **docs/ARCHITECTURE.md rewritten** — mermaid flowchart + sequence
   diagram, package map with file/line ranges, exact data formats
   (activity.jsonl schema, handoff section order, health JSON), heuristics
   in detail (normalization steps, Jaccard formula, 3-failure rule + 0.70,
   temp-GIT_INDEX_FILE snapshot technique, net/gross math with a worked
   example, scoring table, every env override), 9 design decisions with
   rejected alternatives, failure modes copied from AUDIT.md, security &
   privacy, testing strategy.
3. **docs/BUILD_STORY.md** — phase-by-phase table (0–7) with what was
   built, the key problem found, the fix, the tag, and the PHASE-N.md
   link; the three pivots called out (1b self-report failure → hook plugin,
   3b snapshot bypass bug, 4c dashboard removal); plain honesty statement
   (all code written by Cline, raw logs unedited, the one out-of-Cline
   `.clinerules` edit `d931f91` superseded by the Cline-written rule).
4. **demo-trap regenerated honestly** (git-ignored) — a realistic
   concurrent-map TTL store bug with a failing test, `README.md` stating it
   is a SCRIPTED demonstration, and **no sabotage code**: the previous
   `test.sh` (which silently reverted agent fixes) was removed.
   Verified: `go test .` fatals with `fatal error: concurrent map writes`.

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| README quickstart: numbered, every step copy-pasteable | PASS | README.md sections 1–7 |
| Dual MCP registration (make install + manual JSON with Linux/macOS/Windows paths + placeholders + autoApprove) | PASS | README.md step 4 |
| Plugin step + SDK/CLI/Kanban caveat | PASS | README.md step 5 |
| Agent-rule text block | PASS | README.md step 6 |
| Restart + first prompt + expected output | PASS | README.md step 7 |
| Verify-it-works commands + expected output | PASS | README.md "Verify it works" |
| Uninstall + troubleshooting table | PASS | README.md |
| ARCHITECTURE.md: mermaid overview + sequence | PASS | file (both diagrams render) |
| Package map with line ranges | PASS | file |
| Data formats, heuristics detail (formulas, worked example, scoring, env overrides) | PASS | file |
| Design decisions with rejected alternatives | PASS | 9 items |
| Failure modes from AUDIT.md | PASS | MEDIUM-1 + LOW-2..8 + host limits |
| Security/privacy + testing strategy | PASS | file |
| BUILD_STORY.md table phases 0–7 + pivots + honesty statement | PASS | file |
| demo-trap honest (no sabotage) + scripted README | PASS | `go test .` real race; no revert code |
| `make verify` green | PASS | output below |
| Pushed to GitHub (main + phase-7-done tag) | PASS/FAIL | see Deployment |

## Files built

- `README.md` (150) — rewritten.
- `ARCHITECTURE.md` (247) — rewritten.
- `docs/BUILD_STORY.md` (52) — new.
- `demo-trap/` (git-ignored, not committed): `go.mod`, `store.go` (59),
  `trap_test.go` (33), `Makefile`, `test.sh`, `README.md`.

## Design decisions

- **Honest demo-trap:** the demonstration is scripted in the narration, not
  in the code; nothing reverts fixes. The bug is real (`sync.RWMutex`
  declared, never locked) and the test genuinely races.
- **Placeholders only where unavoidable** (manual JSON block), clearly
  marked `REPLACE-WITH-…`; every other command is exactly runnable.
- **ARCHITECTURE diagrams in mermaid** so GitHub renders them natively with
  no image assets.
## Deviations from the prompt

- None material. The spec asked for no Go behavior changes — none were
  made (verified: only docs changed in this phase).
- The previous demo-trap (Phase 5) contained a sabotaging `test.sh`; the
  Phase 7 spec explicitly forbids such code, so it was replaced rather than
  reused.

## Problems encountered

- Editor size limits when creating the large docs — split into sequential
  chunks; one chunk temporarily ate the health-JSON code fence (restored
  immediately; final file verified by reading it back).
- None reaching commits: `make verify` was green at each commit.

## Test evidence

```
OK   fmt-check (nothing to format)
OK   vet
ok 	vault/cmd/vault	0.814s
ok 	vault/internal/heuristics	0.382s
ok 	vault/internal/mcp	0.017s
ok 	vault/internal/state	0.178s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

demo-trap (expected failure, evidence the trap is real):

```
$ cd demo-trap && go test .
fatal error: concurrent map writes
goroutine 71 [running]:
```

## Manual verification

- README commands executed verbatim: `make build` ✓, `bin/vault health` ✓
  (healthy JSON), `bin/vault report --root "$(pwd)" '…'` ✓ (`recorded #N`),
  `make install` ✓ (temp HOME, backup created), `make uninstall` ✓.
- `git check-ignore demo-trap` → ignored ✓.

## Deployment

- Branch `cline/d7a73` committed and tagged `phase-7-done`, fast-forwarded
  into `main` (main is now a strict descendant after the Phase 6 merge, so
  `--ff-only` succeeded), deployed binary rebuilt, `tools/list` re-checked,
  MCP settings verified, logs backed up, pushed to
  `https://github.com/VivekWar/cline-vault.git` with tags.
- `phase-4-done` again rejected on push (already exists on origin; moving
  it requires a force-push, which is forbidden) — reported, not forced.

## Known limitations

- macOS/Windows settings paths are documented from the Cline schema but
  not empirically verified on this Linux machine.
- The demo-trap is git-ignored, so fresh clones must regenerate it (or the
  demo machine keeps its local copy) — intentional, since it must never
  break `make verify`.

## Handoff to next phase

Submission-ready: clone → `make install` → restart Cline → first prompt,
all from README.md. The demo video can use `demo-trap/` (local copy) with
the prepared failing sequence described in `demo-trap/README.md`.

## Metadata

- Commits: `8c2ab9e` (docs), `…` (report/tag)
- Tag: `phase-7-done`
- Branch: `cline/d7a73` → fast-forwarded into `main`

