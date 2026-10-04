# Vault

An MCP server that watches an AI agent for **context rot** — the loops, thrashing, and poisoned context that silently kill long sessions — and tells the agent to stop and hand off before the session burns itself out.

## The problem

Long agent sessions degrade in ways nobody notices until it's too late: the agent repeats the same failing change over and over (error loops), flips between two versions of a fix without converging (thrashing), and fills its context window with stale attempts that poison every later decision. By the time a human sees it, the session is ruined and the work has to restart from scratch. Vault watches the agent's own activity, detects the rot deterministically and offline, and forces a clean handoff to a fresh session.

## What Vault does

- **Records everything the agent does** — every command and file edit lands in an append-only activity log (`.vault/activity.jsonl`) via a telemetry plugin hook.
- **Detects context rot deterministically** — two local heuristics: H1 recurring error loops (Jaccard similarity over failing output) and H2 code oscillation (net-vs-gross git-tree churn). No LLM calls, no network.
- **Verdicts the session** — `check_context_health` returns a plain-language directive (`VERDICT: HEALTHY` / `VERDICT: DEGRADED`) plus a scored health JSON.
- **Writes a resume file** — on `create_handoff` it condenses the activity history into `.vault/handoff_state.md` (with a compression estimate) and rotates the log.
- **Protects secrets** — stderr and handoff output are redacted of API keys, bearer tokens, PEM private keys, and `KEY=value` pairs before anything reaches disk.

## Quickstart

### 1. Prerequisites

```bash
go version    # must print go1.21 or newer
git --version # any recent git
node --version # optional — only needed for the telemetry plugin
```

### 2. Clone

```bash
git clone https://github.com/VivekWar/cline-vault.git && cd cline-vault
```

### 3. Build and smoke test

```bash
make build
bin/vault health
```

Expected output (a healthy empty session):

```json
{"score":100,"status":"healthy","flags":[],"reason":"no context-rot signals detected.","recommendation":"continue","metrics":{...}}
```

### 4. Register the MCP server in Cline

**Option A — one command:**

```bash
make install
```

This verifies Go/git, builds, registers the `vault` MCP server (backing up
your settings file first, never touching your other servers), installs the
plugin's npm dependencies, adds the agent rule to `.clinerules`, and runs
the smoke test.

**Option B — manual:** paste this block into `mcpServers` in Cline's
`cline_mcp_settings.json`. Replace the three `REPLACE-WITH-…` placeholders
with the absolute path of your clone (e.g. `/home/you/cline-vault`).

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

Where that file lives:

- Linux (Cline CLI): `~/.cline/data/settings/cline_mcp_settings.json`
- Linux (VS Code extension): `~/.config/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json`
- macOS (Cline CLI): `~/.cline/data/settings/cline_mcp_settings.json`
- macOS (VS Code extension): `~/Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json`
### 5. Install the telemetry plugin

```bash
cd .cline/plugins/vault-telemetry && npm ci && npm run build
```

Cline discovers the plugin from the project's `.cline/plugins/` folder.
**Important:** the `afterTool` hook only runs in Cline's SDK/CLI/Kanban
hosts — it is not loaded by the VS Code extension, where you record activity
manually with `vault report` (see "Verify it works").

### 6. Add the agent rule

Paste this block into your own `.clinerules` so the agent checks its own
health:

```text
After any failing command, call check_context_health. If the verdict is DEGRADED, stop and call create_handoff. At the start of a new task, call read_handoff.
```

### 7. Restart Cline and try it

Restart Cline (so it reloads the MCP server), then send:

> Call `check_context_health` and show me the verdict. Then call `create_handoff` with goal "fresh start" and next_action "verify the setup".

You should see `VERDICT: HEALTHY. Continue working.` followed by the health
JSON, and then the path of a freshly written `.vault/handoff_state.md`.

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

## Uninstall

```bash
make uninstall   # removes only the vault entry from cline_mcp_settings.json
```

Then delete the clone folder. Nothing else is installed anywhere.

## Troubleshooting

| Symptom | Fix |
|---|---|
| "Not connected" for vault tools | Rebuild (`make build`) and **restart/reload Cline** — MCP servers are spawned from the binary path captured at startup. |
| Workspace path falls back to VAULT_ROOT | The `workspace` argument must be an absolute path inside a git repo (Cline uses git worktrees, which relative paths cannot resolve). |
| Telemetry plugin not recording | The hook only loads in SDK/CLI/Kanban hosts; in other hosts run `vault report '<json>'` manually. After editing the plugin, rerun `npm ci && npm run build`. |
| No churn numbers | The repo must be a git repository — snapshots silently no-op when `git` is missing or the root isn't a repo. |
| Settings file not found | Create the file (or its folder) first, or point the installer at a custom location with `CLINE_MCP_SETTINGS=/path/to/file make install`. |

## Built with Cline

Every line of this project was written by Cline for the hackathon. The
phase-by-phase history is in `docs/phases/`, the build story in
`docs/BUILD_STORY.md`, the architecture in `docs/ARCHITECTURE.md`, and the
pre-flight audit in `docs/AUDIT.md`.

