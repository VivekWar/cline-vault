# Vault

A pure-Go MCP server that watches an AI agent for **context rot** — the recurring error loops and code thrashing that burn tokens — and writes a handoff file so a fresh session can resume cleanly.

## The problem

Long sessions degrade: agents repeat the same failing change over and over, oscillate between two versions of a fix, and burn context on loops instead of progress. No one notices until the session is ruined. Vault watches the agent's own activity, scores the rot, and tells the agent when to stop and hand off.

## Quickstart

```bash
git clone https://github.com/VivekWar/cline-vault.git
cd cline-vault
make install
```

Then **restart Cline** so it picks up the new MCP server, and try:

> Run `go test ./...` and then call `check_context_health` and show me the verdict.

`make install` verifies Go 1.21+ and git, builds `bin/vault`, registers the
`vault` MCP server in Cline's settings (backing up first — your other
servers are never touched), installs the telemetry plugin's npm deps when
node is available, adds a context-health hook to `.clinerules`, and finishes
with a `bin/vault health` smoke test. `make uninstall` removes only the MCP
entry.

## How it works

```
Cline agent ──runs commands/edits──▶ SDK plugin afterTool hook
      │                                  │
      │                        spawns `vault report '<json>'`
      │                                  ▼
      │                      .vault/activity.jsonl (append-only log)
      │                                  │
      │                     local heuristics (H1/H2, no network)
      │                                  ▼
      └── check_context_health ◀── score + verdict (HEALTHY/DEGRADED)
                                          │
                                  DEGRADED? stop, create_handoff
                                          ▼
                                 .vault/handoff_state.md (resume file)
```

## Detection

- **H1 — recurring error loop:** the last 3 failing COMMAND/TEST outputs are
  normalized (paths, numbers, timestamps stripped) and compared pairwise
  with Jaccard similarity; ≥ 0.70 on all three pairs flags
  `RECURRING_ERROR_LOOP`.
- **H2 — code oscillation:** every non-READ activity snapshots the git tree
  (via a temp index, never touching yours); net vs gross churn across
  snapshots below a 0.15 efficiency threshold flags
  `CODE_OSCILLATION_THRASHING`.
- The score starts at 100, subtracts per flag, and maps to `healthy` /
  `degraded` / `critical`.

## Tools

| Tool | What it does |
|---|---|
| `report_activity` | Append one activity record (kind, command, exit code, stderr, files) — called automatically by the telemetry plugin. |
| `check_context_health` | Returns a directive (`VERDICT: HEALTHY` / `VERDICT: DEGRADED`) followed by the health JSON. |
| `create_handoff` | Writes `.vault/handoff_state.md` from your summary + the activity log, then rotates the log into `.vault/archive/`. |
| `read_handoff` | Returns the current handoff state file. |

CLI extras: `vault health` (raw JSON), `vault report --root DIR '<json>'`
(record an activity manually).

## Extras

- **Secret redaction** — stderr and handoff output are scrubbed of API keys,
  bearer tokens, PEM private keys and `KEY=value` pairs before anything hits
  disk.
- **Compression estimate** — every handoff ends with
  `Vault Compression Estimate: Condensed ~X tokens of activity history into
  ~Y tokens of handoff state. (Saved ~Z tokens).`

## Honesty note

The `demo-trap` module used in demos is a deliberately broken Go package
(scripted) so Vault can catch a real error loop on stage. The telemetry
plugin's `afterTool` hook only runs automatically in Cline SDK/CLI/Kanban
hosts; in other hosts record activity manually with `vault report '<json>'`.

## Built with Cline

This project was built entirely with Cline for the hackathon. Phase-by-phase
history lives in `docs/phases/`, the architecture rationale in
`ARCHITECTURE.md`, and the audit in `docs/AUDIT.md`.
