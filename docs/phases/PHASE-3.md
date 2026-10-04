# Vault — Phase 3 Report

## Summary

Phase 3 pivots from manual telemetry to automated telemetry: a local Cline SDK
plugin at `.cline/plugins/vault-telemetry/` that uses the `afterTool` lifecycle
hook to intercept every terminal command the agent runs and append it to
Vault's `.vault/activity.jsonl` — no `.clinerules` prompt compliance needed.

The plugin exports an `AgentPlugin` (`name: "vault-telemetry"`, manifest
capability `hooks`). Its `setup(api, ctx)` resolves the workspace root from the
host's `ctx.workspaceInfo.rootPath` (documented as the correct source — never
`process.cwd()`), and its `hooks.afterTool(context)`:

- matches command-execution tools (`run_commands` in the SDK/CLI,
  `execute_command` in the VS Code extension, plus legacy `run_command`);
- extracts the command string(s), the combined stdout/stderr output, and the
  exit code (parsed from `Command exited with code N` markers or per-item
  `success`/`error` fields);
- writes one JSON line per call — `time`, `kind:"COMMAND"`, `command`,
  `exit_code`, `stderr` (last 40 lines), `files:[]`, `workspace` — directly to
  `<workspace>/.vault/activity.jsonl`.

The hook is strictly observational: it never throws and never modifies tool
results.

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| Plugin project at `.cline/plugins/vault-telemetry/` (package.json, tsconfig.json) | PASS | files exist, committed |
| Exports an `AgentPlugin` with `hooks.afterTool` | PASS | `index.ts`; type-checked against real `@cline/sdk` types |
| Matches command tools (run_commands / execute_command / run_command) | PASS | `COMMAND_TOOL_NAMES` + smoke test |
| Extracts command, exit code, combined stdout/stderr | PASS | `extractCommand`/`extractResult` unit tests |
| Writes exact Vault JSON line (stderr truncated to 40 lines, files:[]) | PASS | `buildLine` tests + end-to-end smoke test |
| Append goes to `<workspace>/.vault/activity.jsonl` | PASS | smoke test wrote one line to the temp workspace |
| `.clinerules` manual telemetry instructions removed, plugin note added | PASS | diff shows the section replaced |
| TS code compiles | PASS | `npm run typecheck` clean, `tsc` emits to dist/ |
| `make verify` green | PASS | output below |

## Files built

- `.cline/plugins/vault-telemetry/index.ts` (185) — the AgentPlugin: setup + afterTool hook + pure helpers (`extractCommand`, `extractResult`, `lastLines`, `buildLine`).
- `.cline/plugins/vault-telemetry/index.test.ts` (111) — 14 node:test cases for the pure helpers.
- `.cline/plugins/vault-telemetry/package.json` (26) — package manifest with `cline.plugins` entry (`./index.ts`, capability `hooks`), devDeps `@cline/sdk`, `typescript`, `@types/node`; scripts `build`/`typecheck`/`test`.
- `.cline/plugins/vault-telemetry/tsconfig.json` (14) — strict TS config, emits to `dist/`.
- `.cline/plugins/vault-telemetry/.gitignore` (3) — `node_modules/`, `dist/`, `*.log`.
- `.cline/plugins/vault-telemetry/package-lock.json` — pinned devDeps.
- Modified: `.clinerules`.

## Design decisions

- **`setup()` captures `workspaceInfo.rootPath`.** The SDK docs explicitly say
  the setup context is "always sourced from the host session config — never
  from `process.cwd()`"; using `import.meta.url` tricks is discouraged. The
  plugin resolves the workspace in `setup()` and closes over it in the hook,
  with `VAULT_ROOT` / `process.cwd()` as fallbacks if setup never ran.
- **Type-only import of `@cline/sdk`.** `import type { AgentPlugin }` is erased
  at transpile time, so the host never has to resolve `@cline/sdk` for this
  plugin; it exists only as a devDependency for local `tsc` checking against
  the real published types (`@cline/sdk@0.0.90`).
- **Both default and named (`plugin`) exports.** The host loader prefers
  `default` and falls back to the named `plugin` export (verified in
  `@cline/core` plugin-module-import source).
- **Exit-code extraction is defensive.** The SDK's `run_commands` result is an
  array of `{ query, result, error?, success }`; failures carry
  `error: "Command exited with code N"`. The plugin parses that, scans the
  output text for the same marker, and falls back to `success ? 0 : 1`,
  tolerating the extension's string/object result shapes too.
- **The hook never throws.** A thrown hook error can count as a tool failure;
  all work is wrapped in try/catch and logged to stderr.
- **`workspace` field included** (one addition beyond the required field list):
  the Go server uses it to snapshot the right git repo for H2 churn.

## Deviations from the prompt

- Added `workspace` to the JSON line (harmless for the Go parser; improves H2
  churn correctness). All required fields are present verbatim.
- Added `index.test.ts` + npm scripts beyond the "basic Node/TS package" ask so
  the extraction logic is table-driven tested, in line with the repo's
  testing rules.
- `package-lock.json` is committed for reproducible devDeps.

## Problems encountered

- **`afterTool` returning `void` fails strict type-checking.** The SDK types
  require `AgentAfterToolResult | Promise<...> | undefined`. Fix: explicit
  `return undefined;` at the end of the hook.
- **`node --test dist/` discovered no tests** in Node 24 when passed a
  directory; the script now uses the explicit glob `dist/*.test.js`.

## Test evidence

- Plugin unit tests: `npm test` → `tsc` clean, then 14/14 pass
  (`extractCommand` 4, `extractResult` 6, `lastLines` 2, `buildLine` 2).
- End-to-end smoke test (loading the compiled plugin, calling `setup` with a
  fake workspace and `afterTool` with a realistic `run_commands` context):
  exactly one line written to `.vault/activity.jsonl`, a `read_files` call
  correctly ignored, exit code 1 parsed, `files: []`, workspace set.

Full `make verify` output (Go side untouched):

```
OK   fmt-check (nothing to format)
OK   vet
ok  	vault/cmd/vault	0.509s
ok  	vault/internal/heuristics	0.253s
ok  	vault/internal/mcp	0.013s
ok  	vault/internal/state	0.105s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

- `npm run typecheck` — clean.
- Smoke test via a standalone Node script (see above).

## Known limitations

- Cline plugins currently apply to the Cline SDK/CLI/Kanban hosts only — the
  VSCode/JetBrains extension does not load them (per the official docs).
- The plugin needs a fresh session/restart for the host to discover
  `.cline/plugins/`.
- If the workspace root has no `.vault/` yet, the plugin creates it (mkdir -p
  semantics), matching the Go server's on-demand behavior.

## Handoff to next phase

Phase 4: verify live telemetry after the plugin loads (activity.jsonl growing
from real commands), then demo-tuning of H1/H2 thresholds against real
telemetry data.

## Metadata

- Commit: `b847c16` (`feat: phase 3 vault telemetry SDK plugin (afterTool hook)`)
- Tag: `phase-3-done`
- Branch: `cline/cbf4d`

## USER ACTION REQUIRED

- Restart Cline / start a new session in this workspace so the host discovers
  and loads the project plugin at `.cline/plugins/vault-telemetry/`. Everything
  else (compile, tests, smoke test, commit, merge, rebuild, settings check,
  log backup) was completed.
