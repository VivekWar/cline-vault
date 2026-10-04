# Vault — Phase 4c Report

## Summary

Phase 4c replaces the static-report UX with a true real-time web dashboard:

1. **`vault serve` CLI command** (`cmd/vault/main.go`). Starts an HTTP
   server (default `:8080`, overridable with `--addr`) with two routes
   registered on the DefaultServeMux:
   - `/` — the base HTML shell: `<head>` with the HTMX script
     (`<script src="https://unpkg.com/htmx.org@1.9.12"></script>`), the
     shared inline CSS, and a `<main>` content area wrapped in the HTMX
     polling div (`hx-get="/content" hx-trigger="every 1s"
     hx-swap="innerHTML"`), pre-filled with the current content so the first
     paint is instant.
   - `/content` — ONLY the inner fragment (stats, flags, churn bars, tables)
     that HTMX swaps in every second.
   `vault serve --root DIR` selects the vault root like every other
   subcommand; the default no-arg behavior of the binary remains the stdio
   MCP server.

2. **`internal/report` rewrite.** One data pipeline (`activity.jsonl` →
   `heuristics.Assess` → `buildPage`) feeds four shared templates: `css`,
   `content`, `dashboard` (live shell) and `static` (the self-contained
   snapshot still written by `vault report` / `Generate`). The live page,
   the static file and the `/content` fragment therefore render the exact
   same numbers.

3. **UI polish.** Premium minimalist light theme (Stripe/Linear style):
   Inter/Roboto UI stack, Fira Code/SF Mono for data, `#fafafa` background,
   soft `#eaeaea` borders, rounded cards with generous whitespace, flat
   pill badges (soft red/dark red for critical, amber for degraded, green
   for healthy), a pulsing LIVE dot, tabular numerals, and churn bars with
   `transition: width 0.3s ease` plus a `growbar` mount keyframe (so the
   bars animate on every HTMX innerHTML swap), with a
   `prefers-reduced-motion` fallback.

## Acceptance criteria

| Criterion | PASS/FAIL/PARTIAL | Evidence |
|---|---|---|
| `vault serve` starts an HTTP server (default :8080, --addr overridable) | PASS | `TestServeCLI` (binds a free port and serves); manual smoke test below |
| `/` serves the shell with the exact HTMX script in `<head>` | PASS | `TestDashboardHandler`, `TestServeCLI` (exact string asserted) |
| Content area wrapped in the polling div (`hx-get=/content`, `every 1s`, `innerHTML`) | PASS | same tests (exact attribute string asserted) |
| `/content` serves ONLY the inner fragment | PASS | `TestContentHandler`, `TestServeCLI` (no `<!DOCTYPE`/`<script`/`<style` allowed) |
| Real-time updates without refresh | PASS | `TestContentLiveUpdates` (fragment 1→2 on next request); `TestServeCLI` live append while server runs; manual smoke test below |
| Premium UI: light theme, pill badges, animated churn bars | PASS | `transition: width 0.3s ease` asserted; `badge-critical`/`badge-healthy`/`badge-degraded` classes; design vars in `cssHTML` |
| Static `vault report` still works and stays self-contained | PASS | `TestGenerateWithErrorLoop` (no external assets), `TestGenerateEmpty` |
| Default no-arg binary still the stdio MCP server | PASS | `TestEndToEnd` (4 tools, unchanged) |
| `make verify` green | PASS | output below |

## Files built

- `cmd/vault/main.go` (158) — `serve` case, `runServe` (routes + `ListenAndServe`), `net/http` import.
- `cmd/vault/main_test.go` (313) — `TestServeCLI` e2e (build, free port, shell/fragment asserts, live update).
- `internal/report/report.go` (445) — rewritten: shared `collect` pipeline, `DashboardHandler`/`ContentHandler`, `Generate`, four templates (`css`, `content`, `dashboard`, `static`).
- `internal/report/report_test.go` (215) — 7 tests incl. httptest handler tests and the live-update test.

## Design decisions

- **Shared "content" template.** The dashboard shell, the static snapshot
  and the `/content` fragment all render `{{template "content" .}}`, so the
  three views can never drift apart and the HTMX swap replaces exactly the
  region the shell initially painted.
- **Static snapshot keeps zero external assets.** The dashboard carries the
  mandated HTMX CDN script; the static `report.html` omits the script and
  all `hx-` attributes so the phase-4 "self-contained" property is preserved
  for the file artifact.
- **Bars animate on innerHTML swaps.** With `hx-swap="innerHTML"` (mandated
  by the spec) every poll recreates the DOM nodes, so a plain `transition`
  alone would never fire. Each new bar therefore also mounts with a
  `growbar` keyframe (0 → target width) while `transition: width 0.3s ease`
  remains for any in-place width changes; `prefers-reduced-motion` disables
  both.
- **`--addr` flag added** beyond the prompt's hardcoded `:8080` (default
  stays `:8080`) so tests and users can bind an ephemeral port without
  touching the code; behavior otherwise matches the spec's
  `http.ListenAndServe(":8080", nil)` shape (DefaultServeMux + nil handler).
## Deviations from the prompt

- `vault report` (bare form) was retained alongside `vault serve`: the
  phase context says the static approach "was rejected", but the existing
  snapshot command and its tests remain functional and harmless; the
  dashboard is now the primary UI.
- `--addr` flag added (see above).
- HTMX version pinned exactly as specified (`htmx.org@1.9.12`, unpkg CDN).
- UI fonts are referenced via font stacks (Inter/Roboto, Fira Code/SF Mono)
  without loading font files, so the only external asset is the mandated
  HTMX script and the static file stays fully offline-capable.

## Problems encountered

- **Template-file assembly churn** (editor splitting large files): one edit
  accidentally consumed the `@media` CSS line and the closing backtick of
  the `cssHTML` raw string (restored immediately), and another edit replaced
  a test function instead of appending (re-added). All caught by
  `make verify`/compile before commit.
- **Smoke-test shell artifacts, not code bugs:** a backgrounded `&` chain
  scoped a `mktemp` variable into a subshell (empty `$R`), and later a
  stale `serve` process held a smoke-test port with a deleted root — the
  confusion initially looked like "live update shows 0". The Go e2e test
  (`TestServeCLI`) and a clean re-run proved the live update works
  (0 → 1 after `recorded #1`).
- No failed commits; `make verify` was green before each commit.

## Test evidence

`go test ./... -v -count=1` (Phase-4c additions):

```
=== RUN   TestDashboardHandler          --- PASS
=== RUN   TestContentHandler            --- PASS
=== RUN   TestContentLiveUpdates        --- PASS
=== RUN   TestGenerateWithErrorLoop     --- PASS
=== RUN   TestGenerateEmpty             --- PASS
=== RUN   TestBuildPageChurnBars        --- PASS
=== RUN   TestBuildPageRedactsCommands  --- PASS
=== RUN   TestServeCLI                  --- PASS (e2e: build + serve + live update)
```

Full `make verify` output:

```
OK   fmt-check (nothing to format)
OK   vet
ok 	vault/cmd/vault	2.693s
ok 	vault/internal/heuristics	0.383s
ok 	vault/internal/mcp	0.023s
ok 	vault/internal/report	0.011s
ok 	vault/internal/state	0.197s
OK   test
OK   build (bin/vault)
verify: all checks passed
```

## Manual verification

Live smoke test against the built binary:

```
$ ./bin/vault serve --root $R --addr 127.0.0.1:18123 &
$ curl -s http://127.0.0.1:18123/ | head   → <!DOCTYPE html>, HTMX <script>,
  <main id="main-content" hx-get="/content" hx-trigger="every 1s"
  hx-swap="innerHTML">, design-system CSS (:root vars, #eaeaea borders)
$ curl -s .../content | grep "Total actions"  → value 0
$ ./bin/vault report --root $R '{"kind":"COMMAND",...}' → recorded #1
$ curl -s .../content | grep "Total actions"  → value 1   ← live update
```

Opening http://localhost:8080 in a browser shows the dashboard polling
`/content` every second; activity.jsonl changes animate in (churn bars grow,
badges flip, tables gain rows).

## Known limitations

- The dashboard's churn numbers come from trees already recorded in
  activity.jsonl (same policy as the static report): no fresh git snapshot
  per poll, keeping 1s polling cheap and deterministic.
- Each poll re-runs `heuristics.Assess`, which diffs recorded trees with
  git; on very large repos with hundreds of snapshots the 1s cadence could
  lag. Demo-scale repos are unaffected.
- HTMX is loaded from unpkg (as mandated); without network access the page
  renders its pre-filled content but stops polling (the static
  `vault report` file remains the offline fallback).
- `serve` binds only the routes `/` and `/content`; unknown paths get Go's
  default 404 page.

## Handoff to next phase

Phase 5 can build on the dashboard: additional sections, filterable
activity, and historical archives are one template away (`internal/report`
`contentHTML`); the HTTP handlers are self-contained `http.HandlerFunc`s
ready for middleware (auth, CORS).

## Metadata

- Commit: `2f2a816` (feature), `a25b908` (docs)
- Tag: `phase-4c-done`
- Branch: `cline/d7a73` — NOT yet fast-forwarded into `main`: the ff-only
  merge was blocked because `main` diverged (parallel Phase 5 session
  committed `afc1588` + `1dd8530` directly to `main`; merge-base `4bd18f0`).
  Per `.clinerules`, stopped and reported; no force/rebase/reset.


