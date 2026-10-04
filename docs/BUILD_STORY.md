# Vault — Build Story

How Vault went from an empty repo to a submitted hackathon project, phase by
phase. Every phase was built by **Cline** from the user's prompt; each one
ended with `make verify` green, a commit, a tag, and a phase report.

| Phase | Built | Key problem found | Fix | Tag | Report |
|---|---|---|---|---|---|
| 0 | Repo scaffold: Makefile (fmt/vet/test/build), `.clinerules`, ARCHITECTURE.md | Empty module needed a green `make verify` baseline | Skip-policy targets that tolerate zero .go files | `phase-0-done` | [PHASE-0](docs/phases/PHASE-0.md) |
| 1 | Minimal stdio JSON-RPC MCP server (initialize/tools/list/tools/call) | stdout pollution corrupts the protocol | Strict rule: JSON-RPC only on stdout, logs to stderr | `phase-1-done` | [PHASE-1](docs/phases/PHASE-1.md) |
| 1b | Activity log, workspace arg, read_handoff, rotation | **The LLM made ZERO `report_activity` calls** when asked to self-report telemetry | Pivot: telemetry must be automatic → SDK plugin (Phase 3) | `phase-1b-done` | [PHASE-1b](docs/phases/PHASE-1b.md) |
| 2 | H1 error-loop (Jaccard), H2 churn (git trees), Health scoring | Thresholds needed env tuning for the demo | All thresholds env-overridable | `phase-2-done` | [PHASE-2](docs/phases/PHASE-2.md) |
| 3 | Telemetry SDK plugin (`afterTool` hook) | Plugin wrote `activity.jsonl` directly | Hook spawns `vault report '<json>'` instead | `phase-3-done` | [PHASE-3](docs/phases/PHASE-3.md) |
| 3b | **Snapshot bypass bug:** plugin's direct file writes starved the `tree` field that H2 churn depends on | Churn was blind to all plugin-recorded activity | `vault report` subcommand; server takes the snapshot at record time | `phase-3b-done` | [PHASE-3b](docs/phases/PHASE-3b.md) |
| 3c | READ rows skip snapshots; micro-oscillation gate (10+ snapshots) | One-line flip-flopping invisible to line thresholds | Snapshot-count gate on H2 | `phase-3c-done` | [PHASE-3c](docs/phases/PHASE-3c.md) |
| 4 | Agent directive verdicts; HTML report; redaction; compression footer | (see 4c) | — | `phase-4-done` | [PHASE-4](docs/phases/PHASE-4.md) |
| 4b | Pre-flight audit of everything built so far | No show-stoppers; 1 MEDIUM + 8 LOW findings, all measured | Documented in AUDIT.md; code untouched for the demo lock | (none) | [AUDIT](docs/AUDIT.md) |
| 4c | Real-time HTMX dashboard (`vault serve`) | **The UI felt broken and was not interactive; the static/dashboard approach didn't work for the demo** | **Pivot: the entire web UI (Phase 4c + Phase 4's HTML report) was removed at the user's request** — Vault is CLI + MCP only | `phase-4-done` (re-pointed) | [PHASE-4](docs/phases/PHASE-4.md) |
| 5 | `demo-trap/`: deliberately broken Go module for live demos | A realistic bug an LLM loops on (concurrent map race) | Multi-layer trap design; git-ignored so `make verify` stays green | `phase-5-done` | [PHASE-5](docs/phases/PHASE-5.md) |
| 6 | One-command install (`make install`), README, GitHub push | Settings JSON merge needed idempotency + backups; main had diverged via a parallel session | Go `mcp-register` subcommand (stdlib-only merge); non-ff merge of the branches | `phase-6-done` | [PHASE-6](docs/phases/PHASE-6.md) |
| 7 | Submission docs: copy-paste README, ARCHITECTURE.md, BUILD_STORY.md, honest demo-trap | Old demo-trap script silently reverted agent fixes — dishonest | Regenerated: realistic bug, no sabotage code, scripted-in-the-narration | `phase-7-done` | [PHASE-7](docs/phases/PHASE-7.md) |

## The pivots that shaped the product

1. **Phase 1b → 3: from "ask the LLM to self-report" to an automatic hook.**
   When the agent was instructed to call `report_activity`, it called it
   zero times. Telemetry had to be invisible to the agent — hence the SDK
   plugin's `afterTool` hook.
2. **Phase 3b: the snapshot bypass bug.** The first plugin wrote JSONL
   directly, bypassing the Go server, so no `tree` field was ever recorded
   and H2 churn was blind. The fix — routing every record through
   `vault report` — is why the plugin spawns the binary today.
3. **Phase 4c: the dashboard removal.** The real-time web dashboard was
   built (HTMX, live UI), felt broken in practice, and was removed
   entirely at the user's request. The lesson landed in ARCHITECTURE.md's
   design decisions: stdio MCP over HTTP, always.

## Honesty statement

- **All code in this repository was written by Cline**, driven by the
  user's phase prompts. The raw, unedited task logs from those sessions are
  archived locally on the build machine (`~/cline-log-backups/`) and in the
  Cline session history; they are not edited or cherry-picked.
- **One `.clinerules` edit was made outside Cline** (commit `d931f91`,
  adding the "Autonomous Health Monitoring" section). It was later
  superseded by the Cline-written rule text merged in Phase 6
  (`After any failing command, call check_context_health…`).
- The `demo-trap/` folder is a **scripted demonstration** — deliberately
  broken, honestly labelled, and containing no code that sabotages or
  reverts an agent's fix. See `demo-trap/README.md`.
