# Vault: Autonomous Context Health & Handoffs for AI Agents

**A single, dependency-free Go binary that watches your coding agent for _context rot_ — the error loops and code thrashing that silently kill long sessions — and forces a clean handoff before the session burns itself out.**

[![Go](https://img.shields.io/badge/go-1.21%2B-00ADD8?logo=go&logoColor=white)](#quickstart)
[![dependencies](https://img.shields.io/badge/dependencies-zero-brightgreen)](#quickstart)
[![protocol](https://img.shields.io/badge/protocol-MCP%20over%20stdio-6E56CF)](#quickstart)
[![detection](https://img.shields.io/badge/detection-offline%20%C2%B7%20deterministic-blue)](#how-detection-works)

> 📐 **Deep dive:** [**docs/ARCHITECTURE_AND_DESIGN.md**](docs/ARCHITECTURE_AND_DESIGN.md) — the full system architecture, the mathematics of context rot, the autonomous-intervention protocol, and the security model.

---

## Table of contents

- [The problem: context rot is the silent killer of agentic coding](#the-problem-context-rot-is-the-silent-killer-of-agentic-coding)
- [The solution: Vault](#the-solution-vault)
- [Why judges should care](#why-judges-should-care)
- [Quickstart](#quickstart)
- [Verify it works](#verify-it-works)
- [How detection works](#how-detection-works)
- [Uninstall](#uninstall)
- [Troubleshooting](#troubleshooting)
- [Built with Cline](#built-with-cline)

---

## The problem: context rot is the silent killer of agentic coding

When an autonomous coding agent hits a bug it cannot solve on the first try, a predictable failure mode begins — and it stays invisible until the damage is done:

1. **It retries the same failing change.** The command fails, the agent "fixes" it, the command fails again with near-identical output. Every attempt dumps another wall of error logs into the context window.
2. **It thrashes.** The agent flip-flops between two versions of a fix — reverting A to apply B, then reverting B to apply A — without ever converging.
3. **Its context becomes poisoned.** Stale, failed attempts crowd out the actual task. The agent starts reasoning about its own wreckage instead of the problem.
4. **It hallucinates.** Confidence rises as accuracy collapses. The agent invents APIs, "remembers" files it never read, and declares success on a broken build.

By the time a human notices, the session is ruined and the work has to restart from scratch — with all the tokens, time, and momentum lost. This is **context rot**, and it is the single most expensive failure mode in agentic coding.

## The solution: Vault

**Vault is an MCP server that runs silently in the background and monitors the agent's own telemetry.** It is not a prompt, not a tool the agent has to remember to call, and not a network service — it is a small, deterministic guardrail wired directly into the agent's tool loop.

- **It records everything the agent does.** A Cline SDK plugin hooks `afterTool` and streams every command and edit to Vault's append-only log (`.vault/activity.jsonl`) — automatically, with no cooperation from the agent.
- **It detects rot mathematically.** Two local heuristics run over that log: **H1** catches recurring error loops with Jaccard similarity over normalized output, and **H2** catches code oscillation with net-vs-gross git-tree churn. No LLM, no network, fully deterministic and reproducible.
- **It intervenes.** When rot is detected, the `check_context_health` tool returns a **`VERDICT: DEGRADED`** directive telling the agent to stop immediately.
- **It saves the session.** `create_handoff` condenses the entire activity history into a structured `.vault/handoff_state.md` resume file, rotates the log, and lets the next session start clean.
- **It protects secrets.** A deterministic RE2 redaction pass scrubs API keys, bearer tokens, and PEM private keys before anything is written to disk.

The result: a session that would have burned itself out is caught mid-loop, summarized, and restarted — automatically.

## Why judges should care

| Criterion | How Vault delivers |
|---|---|
| **Real, painful problem** | Context rot is a universal failure mode of every long-running coding agent. |
| **Non-trivial engineering** | Deterministic heuristics, safe git-tree snapshots, an MCP server, an SDK hook, and a redaction engine — all in the Go standard library. |
| **Mathematically grounded** | H1 uses Jaccard similarity; H2 uses a net/gross churn ratio. Both are explainable and reproducible — see the [math section](docs/ARCHITECTURE_AND_DESIGN.md#2-the-mathematics-of-context-rot). |
| **Trivially easy to use** | One `make build`, one JSON block, one restart. Zero external dependencies. |
| **Privacy by design** | Fully offline. Nothing leaves the machine. Secrets are redacted before they ever touch disk. |
| **Built with Cline** | Every line was written by Cline during the hackathon — and Vault was dogfooded on itself throughout the build. |

## Quickstart

### 1. Prerequisites

```bash
go version      # must print go1.21 or newer
git --version   # any recent git
node --version  # optional — only needed for the automatic telemetry plugin
```

### 2. Clone

```bash
git clone https://github.com/VivekWar/cline-vault.git
cd cline-vault
```

### 3. Build the binary

```bash
make build
```

This compiles a single, dependency-free binary to `bin/vault`. Smoke-test it:

```bash
bin/vault health
```

Expected output (a healthy, empty session):

```json
{"score":100,"status":"healthy","flags":[],"reason":"no context-rot signals detected.","recommendation":"continue","metrics":{"similarities":[],"net":0,"gross":0,"efficiency":0,"activity_count":0}}
```

### 4. Register the MCP server in Cline

**Option A — one command (recommended):**

```bash
make install
```

`make install` verifies Go ≥ 1.21 and git, builds `bin/vault`, registers the `vault` MCP server (backing up your settings file first and never touching your other servers), installs the plugin's npm dependencies when Node is present, adds the agent rule to `.clinerules`, and runs the smoke test.

**Option B — manual:** paste the block below into the `mcpServers` object of Cline's `cline_mcp_settings.json`. Replace the three `REPLACE-WITH-…` placeholders with the **absolute path** of your clone (for example `/home/you/cline-vault`).

```json
"vault": {
  "transport": {
    "type": "stdio",
    "command": "REPLACE-WITH-ABS-PATH-TO-cline-vault/bin/vault",
    "cwd": "REPLACE-WITH-ABS-PATH-TO-cline-vault",
    "env": { "VAULT_ROOT": "REPLACE-WITH-ABS-PATH-TO-cline-vault" }
  },
  "autoApprove": ["report_activity", "check_context_health", "create_handoff", "read_handoff"],
  "disabled": false
}
```

Where `cline_mcp_settings.json` lives:

| Platform | Path |
|---|---|
| Linux (Cline CLI) | `~/.cline/data/settings/cline_mcp_settings.json` |
| Linux (VS Code extension) | `~/.config/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json` |
| macOS (Cline CLI) | `~/.cline/data/settings/cline_mcp_settings.json` |
| macOS (VS Code extension) | `~/Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json` |
| Windows (VS Code extension) | `%APPDATA%\Code\User\globalStorage\saoudrizwan.claude-dev\settings\cline_mcp_settings.json` |

### 5. Install the telemetry plugin (automatic recording)

```bash
cd .cline/plugins/vault-telemetry
npm ci && npm run build
```

Cline discovers the plugin from the project's `.cline/plugins/` folder. The `afterTool` hook loads only in Cline's **SDK / CLI / Kanban** hosts — in the VS Code extension, record activity manually with `bin/vault report` (see [Verify it works](#verify-it-works)).

### 6. Add the agent rule

Paste this into your own `.clinerules` so the agent checks its own health:

```text
After any failing command, call check_context_health. If the verdict is DEGRADED, stop and call create_handoff. At the start of a new task, call read_handoff.
```

### 7. Restart Cline and try it

Restart Cline so it spawns the MCP server, then send:

> Call `check_context_health` and show me the verdict. Then call `create_handoff` with goal "fresh start" and next_action "verify the setup".

You should see `VERDICT: HEALTHY. Continue working.` followed by the health JSON, then the path of a freshly written `.vault/handoff_state.md`.

## Verify it works

```bash
# 1. Record one activity manually (the plugin does this automatically in SDK/CLI hosts)
bin/vault report --root "$(pwd)" '{"kind":"COMMAND","command":"echo hello","exit_code":0,"stderr":"","files":[]}'
# → recorded #1

# 2. Check health through the CLI
bin/vault health
# → {"score":100,"status":"healthy",...}

# 3. Watch the log grow
cat .vault/activity.jsonl
```

## How detection works

Vault runs two deterministic, offline heuristics over the activity log and combines them into a 0–100 health score:

| Heuristic | Signal | Trigger (defaults) |
|---|---|---|
| **H1 — Recurring Error Loop** | `RECURRING_ERROR_LOOP` | The last **3** failed `COMMAND`/`TEST` outputs are pairwise **Jaccard-similar ≥ 0.70** |
| **H2 — Code Oscillation** | `CODE_OSCILLATION_THRASHING` | **net/gross churn < 0.15** and (gross > 100 lines **or** ≥ 10 snapshots) |

- A detected loop subtracts **50** points; oscillation subtracts **35**. `status` is `healthy` (≥ 70), `degraded` (≥ 40), or `critical`.
- Every threshold is overridable via environment variables (`VAULT_JACCARD_MIN`, `VAULT_CHURN_MIN_GROSS`, `VAULT_CHURN_MAX_EFF`, `VAULT_CHURN_MIN_SNAPSHOTS`).
- The full derivation — normalization pipeline, Jaccard formula, a worked churn example, and the safe `GIT_INDEX_FILE` snapshot technique — is in the [master architecture document](docs/ARCHITECTURE_AND_DESIGN.md#2-the-mathematics-of-context-rot).

## Uninstall

```bash
make uninstall   # removes only the vault entry from cline_mcp_settings.json
```

Then delete the clone folder. Nothing else is installed anywhere.

## Troubleshooting

| Symptom | Fix |
|---|---|
| "Not connected" for vault tools | Rebuild (`make build`) and **restart/reload Cline** — MCP servers are spawned from the binary path captured at startup. |
| Workspace path falls back to `VAULT_ROOT` | The `workspace` argument must be an **absolute** path inside a git repo (Cline runs in git worktrees, which relative paths cannot resolve). |
| Telemetry plugin not recording | The hook only loads in SDK/CLI/Kanban hosts; in other hosts run `bin/vault report '<json>'` manually. After editing the plugin, rerun `npm ci && npm run build`. |
| No churn numbers | The repo must be a git repository — snapshots silently no-op when `git` is missing or the root is not a repo. |
| Settings file not found | Create the file (or its folder) first, or point the installer at a custom location with `CLINE_MCP_SETTINGS=/path/to/file make install`. |

## Built with Cline

Vault was built **entirely with [Cline](https://github.com/cline/cline)** — the open-source autonomous coding agent — as a hackathon project about keeping coding agents healthy.

- **Every line was written by Cline.** The Go code, the TypeScript SDK plugin, the tests, and the documentation were all produced by Cline from the user's phase prompts: plan, implement, run `make verify`, commit, repeat.
- **Dogfooded from the inside.** Cline used Vault's own MCP tools (`report_activity`, `check_context_health`, `create_handoff`, `read_handoff`) while building Vault, so the project instrumented itself the whole time.
- **Every phase ended green.** The full build story — phases, problems found, pivots, tags, and phase reports — is in [docs/ARCHITECTURE_AND_DESIGN.md](docs/ARCHITECTURE_AND_DESIGN.md#5-hackathon-build-story) and [`docs/phases/`](docs/phases/).
- **Cline even ran the security audit.** The pre-flight architectural and security audit ([`docs/AUDIT.md`](docs/AUDIT.md)) measured the race detector, file-descriptor stability, parallel appenders, temp-file leaks under forced git failures, oversized-line memory, and adversarial redaction timing.

### For judges

This project is a working demonstration of the loop it was built to break: an AI agent (Cline) wrote a tool (Vault) that detects when an AI agent is stuck in an error loop or thrashing code — and then used that tool on itself while building it.

1. Follow the [Quickstart](#quickstart) above.
2. Read [**docs/ARCHITECTURE_AND_DESIGN.md**](docs/ARCHITECTURE_AND_DESIGN.md) for exactly how the detection math and the intervention protocol work.
3. Read [`docs/BUILD_STORY.md`](docs/BUILD_STORY.md) for exactly how Cline built it, phase by phase.

