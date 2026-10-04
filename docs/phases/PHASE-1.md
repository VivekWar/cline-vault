# Phase 1 — Stdio MCP Server Skeleton

Status: **DONE** — 2026-10-04. Working MCP server over stdio; health is a stub.

## Goal

A working MCP server over stdio (JSON-RPC 2.0, newline-delimited messages) at
`cmd/vault`, with logic in `internal/mcp` and `internal/state`, exposing 3
tools: `report_activity`, `check_context_health` (stub), `create_handoff`.

## Decisions

- **Transport**: `bufio.Reader.ReadBytes('\n')` loop (not `bufio.Scanner`).
  Lines over 4 MB (`DefaultMaxLineBytes = 4<<20`, injectable for tests) get a
  `-32700` error with `id:null`, are discarded, and the server keeps serving.
- **Testability**: `mcp.NewServer(r io.Reader, w io.Writer, root string)` —
  the loop is testable without spawning a process; `cmd/vault/main.go` is a
  thin shell around it.
- **stdout discipline**: every response is one `json.Marshal` + one `Write`
  including the trailing newline; all logging via `log` on stderr.
- **id echo**: raw `json.RawMessage` id echoed byte-for-byte (works for
  string, number, and null ids). Notifications (no `id` key) never respond.
- **ping**: result serialized as `{}` (`json.RawMessage("{}")`), never null.
- **Tool failures** are `isError:true` in the `tools/call` result, never a
  JSON-RPC error.
- **Root**: `VAULT_ROOT` env else cwd, resolved with `filepath.Abs`;
  `main.go` does NOT pre-create `.vault/` — `internal/state` creates it on
  demand at the first write.
- **Handoff**: exactly 8 H2 sections in fixed order; touched files = deduped
  union of `files` from `activity.jsonl` plus the note
  "AST scope: not computed (paths only)"; Active Errors = stderr of the last
  3 failing entries (exit_code != 0), 40 lines each; Git State = `git
  rev-parse --short HEAD` + `git status --porcelain` in root ("git
  unavailable" on failure, never an error). Writes are atomic (temp + rename).
- **E2E test** builds its own binary into `t.TempDir()` (make verify runs
  test BEFORE build, so `bin/vault` cannot be assumed to exist).

## Prompt used (summary)

Full protocol spec for the 3 tools, hard acceptance criteria (make verify,
pipe smoke test, MCP settings snippet, PHASE-1.md, memory bank, commit+tag),
with 7 amendments: e2e builds its own binary; `ReadBytes` not `Scanner` with
oversized-line `-32700` test; no `.vault/` pre-creation; ping `{"result":{}}`
never null; raw id echo incl. string id; AST-scope note line; single-Write
line discipline.

## What broke and how it was fixed

1. **TestOversizedLine false failure** — with the injected limit of 32 bytes,
   the *ping* line itself (43 bytes) was oversized, so the server correctly
   returned two `-32700`s and the "keep serving" assertion failed. The server
   was right; the test limit was wrong. Fixed by injecting `maxLine = 64`
   (ping fits, the 200-byte garbage line doesn't).
2. **TDD red phase** — tests first, as planned: `undefined: State/NewServer`
   until the implementation landed. Expected; resolved by writing the code.
3. **Large test file** — `mcp_test.go` exceeded the single-edit size limit;
   split into two writes. Process issue, not a code defect.

## Test evidence — `make verify`

```
$ make verify
OK   fmt-check (nothing to format)
OK   vet
ok  	vault/cmd/vault	0.165s
ok  	vault/internal/mcp	0.008s
ok  	vault/internal/state	0.006s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

Smoke test (acceptance criterion 2) printed exactly 2 JSON lines:

```
$ printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}\n{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}\n' | VAULT_ROOT=$(mktemp -d) ./bin/vault 2>/dev/null | wc -l
2
```

---

# Phase 1b addendum — worktree-awareness, read_handoff, activity rotation

## (a) Manual verification results (observed before the fixes)

1. **MCP server registered with 3 tools** — `tools/list` reported
   `report_activity`, `check_context_health`, `create_handoff`. A live
   `tools/call` succeeded.
2. **Resume test FAILED** — a fresh task in its own git worktree
   (`~/.cline/worktrees/<id>/ClineAiHackathon`, branch `cline/<id>`) could not
   find `.vault/handoff_state.md` by relative path, and `git` commands run
   against `VAULT_ROOT` did not see the agent's edits. Root cause: the server
   is started once with `VAULT_ROOT` = the main checkout, but each task runs in
   a separate worktree, so worktree-relative state and git state were missing.
3. **Telemetry risk test** — 6 activity entries were logged for 5 actions: the
   calls were **batched** (identical timestamps), and one **chained** command
   hid a failure (reported `exit_code 0` because only the last command's status
   was captured).

## (b) Fixes made and reasons

| Fix | Reason |
|---|---|
| Optional `workspace` arg (absolute path) on `report_activity`, `check_context_health`, `create_handoff`; stored per activity entry | Git must run in the agent's worktree, not the main checkout. |
| `gitDir`: use `workspace` only when it is absolute AND inside a git repo; otherwise fall back to `VAULT_ROOT` and log the reason to stderr (never error) | A stale/missing/non-repo workspace must not break the tool. |
| `create_handoff` git section now includes `workspace`, `branch`, `commit`, `status` | Needed to recover which worktree/branch produced the handoff. |
| New `read_handoff` tool (returns full `handoff_state.md`, else `isError:true` "no handoff yet") | Fresh tasks must be able to locate/resume via the shared `VAULT_ROOT/.vault`. |
| Activity rotation: after a successful handoff write, move `activity.jsonl` to `.vault/archive/activity-<UTC>.jsonl`; report archive path (or "none") | Next session starts clean; shared state survives worktree deletion. |
| Handoff format: `# Vault Handoff` title + resume line first; empty sections render `_none_` | Machine-readable resumption instructions. |
| `.clinerules` telemetry section: ONE command per terminal call, no `;`/`&&`, no batching, report verbatim `exit_code`, pass `workspace`; plus Process rule to `--ff-only` merge main and rebuild after a green commit | Prevents the batched/chained-command telemetry risk observed above. |

## Test evidence — `make verify` (green)

```
OK   fmt-check (nothing to format)
OK   vet
ok  	vault/cmd/vault	0.239s
ok  	vault/internal/mcp	0.012s
ok  	vault/internal/state	0.088s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

Commit: `feat: worktree-aware workspace arg, read_handoff, activity rotation`
(2299469), tag `phase-1b-done`.

