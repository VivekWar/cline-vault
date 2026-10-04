# Vault — Phase 3b Report

## Summary

Phase 3b patches the telemetry blindspot: the Phase 3 plugin only intercepted
shell commands, so file edits made without running commands were invisible to
Vault. H2 (Churn) relies on git snapshots taken after activity entries, so pure
edit sessions could not flag thrashing.

The plugin now also intercepts file-edit tools. In `hooks.afterTool`, after the
untouched command branch, edit tools (`write_to_file`, `replace_file_content`,
`edit_file`, `insert_content`) produce an `"EDIT"` entry: `kind` is `"EDIT"`,
`command` is `"edited " + filePath` (path extracted from `file_path` or `path`
in the tool input), `exit_code` is 0, and the line is appended to
`.vault/activity.jsonl` exactly like command entries. The edited path is also
recorded in `files` so `create_handoff`'s Touched Files section stays populated.

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| `EDIT_TOOL_NAMES` set with the four required names | PASS | `index.ts` |
| Edit tools produce kind `"EDIT"`, command `"edited <path>"`, exit_code 0 | PASS | smoke test + `buildLine: EDIT kind line shape` |
| Path extracted from `file_path` or `path` | PASS | `extractFilePath` unit tests (4 cases) |
| Command interception unchanged | PASS | existing `extractCommand`/`extractResult` tests + smoke test line 0 |
| Non-command, non-edit tools still ignored | PASS | smoke test (read_files ignored) |
| Tests cover at least one edit scenario | PASS | 5 new test cases; 19/19 pass |
| TS compiles | PASS | `npm run typecheck` clean |
| `make verify` green | PASS | output below |

## Files built

- `.cline/plugins/vault-telemetry/index.ts` (219, was 185) — added
  `EDIT_TOOL_NAMES`, `extractFilePath`, generalized `CommandRecord` →
  `ActivityRecord` (`kind`, `files`), and the edit branch in `afterTool`.
- `.cline/plugins/vault-telemetry/index.test.ts` (150, was 111) — 4
  `extractFilePath` cases, an EDIT-kind `buildLine` shape test, and updated the
  two existing `buildLine` tests for the new record fields.

## Design decisions

- **`files: [filePath]` on EDIT entries** (small addition beyond the required
  field list): Vault's `create_handoff` renders the Touched Files section from
  the `files` field, so edit entries now feed it; harmless to the Go parser.
- **`file_path` wins over `path`** when both are present, keeping the
  precedence deterministic.
- **Edit branch is independent and after the command branch** — the command
  interception code was not modified at all, per the task.

## Deviations from the prompt

- None beyond the documented `files` addition.

## Problems encountered

- None. The record type change (`CommandRecord` → `ActivityRecord` with `kind`)
  was mechanical; the two existing `buildLine` tests were updated in the same
  change.

## Test evidence

- Plugin unit tests: `npm test` → `tsc` clean, 19/19 pass (command extraction
  4, file-path extraction 4, result extraction 6, lastLines 2, buildLine 3).
- End-to-end smoke test: one command call, one `write_to_file` call, one
  `edit_file` call, and one `read_files` call → exactly 3 lines written;
  EDIT lines carry `"edited <path>"`, exit_code 0, and `files: [<path>]`;
  `read_files` ignored.

Full `make verify` output:

```
OK   fmt-check (nothing to format)
OK   vet
ok  	vault/cmd/vault	0.450s
ok  	vault/internal/heuristics	0.187s
ok  	vault/internal/mcp	0.013s
ok  	vault/internal/state	0.097s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

- `npm run typecheck` — clean.
- Smoke test via a standalone Node script (see above).

## Known limitations

- The plugin keys off tool names; edit tools with other names (e.g. the SDK's
  `editor` / `apply_patch` tools) are still not intercepted.
- Cline plugins still only load in the SDK/CLI/Kanban hosts.

## Handoff to next phase

Phase 4: verify live telemetry (commands + edits) after the plugin loads, then
demo-tuning of H1/H2 thresholds against real telemetry.

## Metadata

- Commit: `4bc554c` (`feat: phase 3b telemetry blindspot patch (EDIT tool interception)`)
- Tag: `phase-3b-done`
- Branch: `cline/cbf4d`

## USER ACTION REQUIRED

- Restart Cline / start a new session in this workspace so the host loads the
  updated plugin. Everything else (compile, tests, smoke test, commit, merge,
  rebuild, settings check, log backup) was completed.
