// Package report renders Vault's web dashboard and reports:
//
//   - a real-time HTMX dashboard (HTTP handlers for / and /content),
//   - a static self-contained snapshot (.vault/report.html via Generate).
//
// All views share one data pipeline: .vault/activity.jsonl is parsed, fed to
// the same heuristics.Assess aggregation that check_context_health uses, and
// rendered through a shared "content" template so the live dashboard and the
// static file can never disagree.
package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"vault/internal/heuristics"
)

// activityLine mirrors one JSON line of .vault/activity.jsonl.
type activityLine struct {
	Time      string   `json:"time"`
	Kind      string   `json:"kind"`
	Command   string   `json:"command"`
	ExitCode  int      `json:"exit_code"`
	Stderr    string   `json:"stderr"`
	Files     []string `json:"files"`
	Workspace string   `json:"workspace"`
	Tree      string   `json:"tree"`
}

// flagView is one triggered context-rot flag with its evidence as JSON.
type flagView struct {
	Name     string
	Evidence string
}

// churnView carries the net-vs-gross churn breakdown plus bar widths.
type churnView struct {
	Net        float64
	Gross      float64
	Efficiency float64
	NetPct     int
	GrossPct   int
}

// loopView summarizes the recurring error loop, when one was detected.
type loopView struct {
	Detected     bool
	Similarities []float64
	SharedTokens []string
}

// rowView is one row of the recent-activity table (command redacted).
type rowView struct {
	Time     string
	Kind     string
	Command  string
	ExitCode int
}

// pageData is the template data for every view.
type pageData struct {
	Total    int
	Failing  int
	Score    int
	Status   string
	Reason   string
	Flags    []flagView
	Churn    churnView
	Loop     loopView
	Recent   []rowView
	HasFlags bool
}

// Generate reads root/.vault/activity.jsonl and writes a self-contained
// .vault/report.html snapshot (inline CSS, no external assets — a plain
// static capture of the same dashboard content `vault serve` streams live).
// It returns the absolute path written. A missing activity log yields an
// empty report.
func Generate(root string) (string, error) {
	data, err := collect(root)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpls.ExecuteTemplate(&buf, "static", data); err != nil {
		return "", err
	}
	vault := filepath.Join(root, ".vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(vault, "report.html")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// DashboardHandler serves the full dashboard shell for "/": the <head> with
// the HTMX script and the main content area wrapped in the HTMX polling div,
// pre-filled with the current content so the first paint is instant.
func DashboardHandler(root string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderTemplate(w, root, "dashboard")
	}
}

// ContentHandler serves ONLY the inner content fragment for "/content" —
// the stats, flags, churn bars and tables that HTMX swaps in every second.
func ContentHandler(root string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderTemplate(w, root, "content")
	}
}

// renderTemplate collects fresh data and executes the named template.
func renderTemplate(w http.ResponseWriter, root, name string) {
	data, err := collect(root)
	if err != nil {
		http.Error(w, "vault: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpls.ExecuteTemplate(w, name, data); err != nil {
		fmt.Fprintf(os.Stderr, "vault report: render %s: %v\n", name, err)
	}
}

// collect reads the activity log and aggregates it into pageData.
func collect(root string) (pageData, error) {
	acts, err := readActivities(filepath.Join(root, ".vault", "activity.jsonl"))
	if err != nil {
		return pageData{}, err
	}
	entries := make([]heuristics.Entry, 0, len(acts))
	trees := make([]string, 0, len(acts))
	failing := 0
	for _, a := range acts {
		entries = append(entries, heuristics.Entry{
			Kind:     a.Kind,
			ExitCode: a.ExitCode,
			Output:   a.Stderr,
		})
		if a.Tree != "" {
			trees = append(trees, a.Tree)
		}
		if a.ExitCode != 0 {
			failing++
		}
	}
	health := heuristics.Assess(root, entries, trees)
	return buildPage(acts, health, failing), nil
}

// readActivities parses activity.jsonl, skipping unparseable lines and
// tolerating a missing file (empty report).
func readActivities(path string) ([]activityLine, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []activityLine
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e activityLine
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // tolerate corrupt lines
		}
		out = append(out, e)
	}
	return out, nil
}

// buildPage converts activities and the health assessment into template data.
func buildPage(acts []activityLine, health heuristics.Health, failing int) pageData {
	data := pageData{
		Total:   len(acts),
		Failing: failing,
		Score:   health.Score,
		Status:  health.Status,
		Reason:  health.Reason,
		Flags:   make([]flagView, 0, len(health.Flags)),
		Churn: churnView{
			Net:        health.Metrics.Net,
			Gross:      health.Metrics.Gross,
			Efficiency: health.Metrics.Efficiency,
		},
		Recent: make([]rowView, 0, len(acts)),
	}
	for _, f := range health.Flags {
		ev, err := json.Marshal(f.Evidence)
		if err != nil {
			ev = []byte("{}")
		}
		data.Flags = append(data.Flags, flagView{Name: f.Name, Evidence: string(ev)})
		if f.Name == "RECURRING_ERROR_LOOP" {
			if le, ok := f.Evidence.(heuristics.LoopEvidence); ok {
				data.Loop = loopView{
					Detected:     true,
					Similarities: le.Similarities,
					SharedTokens: le.SharedTokens,
				}
			}
		}
	}
	data.HasFlags = len(data.Flags) > 0

	// Bar widths for the net-vs-gross visual: percentages of the larger of
	// the two (0/0 renders two empty bars).
	max := data.Churn.Net
	if data.Churn.Gross > max {
		max = data.Churn.Gross
	}
	if max > 0 {
		data.Churn.NetPct = int(data.Churn.Net / max * 100)
		data.Churn.GrossPct = int(data.Churn.Gross / max * 100)
	}

	// Recent activity table: newest last is confusing for readers, so show
	// the most recent rows (capped) with commands redacted.
	start := 0
	if len(acts) > 20 {
		start = len(acts) - 20
	}
	for _, a := range acts[start:] {
		data.Recent = append(data.Recent, rowView{
			Time:     a.Time,
			Kind:     a.Kind,
			Command:  heuristics.Redact(a.Command),
			ExitCode: a.ExitCode,
		})
	}
	return data
}

// tpls holds the named templates: css (shared styles), content (the inner
// fragment), dashboard (the live shell) and static (the standalone snapshot
// file). Dashboard and static both embed the css and content templates, so
// the live page, the static file and the /content fragment share one source
// of truth.
var tpls = func() *template.Template {
	t := template.New("root")
	template.Must(t.New("css").Parse(cssHTML))
	template.Must(t.New("content").Parse(contentHTML))
	template.Must(t.New("dashboard").Parse(dashboardHTML))
	template.Must(t.New("static").Parse(staticHTML))
	return t
}()

// cssHTML is the shared design system: a clean light theme with generous
// whitespace, soft gray borders, flat pill badges, tabular figures and
// animated churn bars (transition + mount keyframe so innerHTML swaps still
// animate smoothly).
const cssHTML = `
:root {
  --bg: #fafafa;
  --card: #ffffff;
  --border: #eaeaea;
  --text: #18181b;
  --muted: #71717a;
  --accent: #635bff;
  --green: #15803d;
  --amber: #b45309;
  --red: #b91c1c;
  --sans: Inter, Roboto, -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
  --mono: "Fira Code", "SF Mono", "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--text); font-family: var(--sans); -webkit-font-smoothing: antialiased; text-rendering: optimizeLegibility; }
.page { max-width: 1024px; margin: 0 auto; padding: 56px 32px 40px; }
.masthead { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 44px; }
.brand { font-size: 22px; font-weight: 650; letter-spacing: -0.02em; }
.brand .accent { color: var(--accent); }
.live { display: inline-flex; align-items: center; gap: 8px; color: var(--muted); font-size: 12px; font-weight: 500; letter-spacing: 0.08em; text-transform: uppercase; }
.live .dot { width: 8px; height: 8px; border-radius: 50%; background: #22c55e; animation: pulse 2s infinite; }
@keyframes pulse { 0% { box-shadow: 0 0 0 0 rgba(34,197,94,.45); } 70% { box-shadow: 0 0 0 8px rgba(34,197,94,0); } 100% { box-shadow: 0 0 0 0 rgba(34,197,94,0); } }
.stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; margin-bottom: 20px; }
.stat { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 20px 22px; }
.stat .label { font-size: 12px; font-weight: 500; letter-spacing: 0.06em; text-transform: uppercase; color: var(--muted); margin-bottom: 10px; }
.stat .value { font-size: 28px; font-weight: 650; letter-spacing: -0.02em; font-variant-numeric: tabular-nums; }
.card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px 26px; margin-bottom: 16px; }
.card h2 { margin: 0 0 18px; font-size: 15px; font-weight: 600; letter-spacing: -0.01em; }
.badge { display: inline-flex; align-items: center; border-radius: 999px; padding: 3px 10px; font-size: 12px; font-weight: 500; line-height: 1.5; }
.badge-healthy { background: #ecfdf5; color: var(--green); border: 1px solid #d1fae5; }
.badge-degraded { background: #fffbeb; color: var(--amber); border: 1px solid #fde68a; }
.badge-critical { background: #fef2f2; color: var(--red); border: 1px solid #fecaca; }
.badge-muted { background: #f4f4f5; color: var(--muted); border: 1px solid var(--border); }
.flag { border: 1px solid var(--border); border-left: 3px solid var(--red); border-radius: 10px; padding: 14px 16px; margin-bottom: 10px; background: var(--card); }
.flag .name { font-weight: 600; font-size: 14px; display: flex; justify-content: space-between; align-items: center; gap: 10px; }
.flag pre { font-family: var(--mono); font-size: 12px; color: var(--muted); margin: 8px 0 0; overflow-x: auto; }
.bar-row { margin-bottom: 16px; }
.bar-meta { display: flex; justify-content: space-between; font-size: 13px; margin-bottom: 6px; }
.bar-meta .val { font-family: var(--mono); font-size: 12.5px; color: var(--muted); }
.bar-track { background: #f4f4f5; border-radius: 999px; height: 10px; overflow: hidden; }
.bar-fill { height: 100%; border-radius: 999px; transition: width 0.3s ease; animation: growbar 0.4s ease-out; }
.bar-net { background: var(--accent); }
.bar-gross { background: #a78bfa; }
@keyframes growbar { from { width: 0; } }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th { text-align: left; font-size: 11px; font-weight: 500; letter-spacing: 0.06em; text-transform: uppercase; color: var(--muted); padding: 8px 10px; border-bottom: 1px solid var(--border); }
td { padding: 9px 10px; border-bottom: 1px solid var(--border); font-variant-numeric: tabular-nums; vertical-align: top; }
tr:last-child td { border-bottom: none; }
.mono { font-family: var(--mono); font-size: 12.5px; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 12px; }
.chip { font-family: var(--mono); font-size: 11.5px; background: #f4f4f5; border: 1px solid var(--border); border-radius: 999px; padding: 2px 9px; color: var(--muted); }
.muted { color: var(--muted); }
.empty { color: var(--muted); font-size: 13px; }
.exit-ok { color: var(--green); font-weight: 600; }
.exit-bad { color: var(--red); font-weight: 600; }
.foot { margin-top: 32px; color: var(--muted); font-size: 12px; display: flex; justify-content: space-between; }
.foot code { font-family: var(--mono); background: #f4f4f5; border: 1px solid var(--border); border-radius: 6px; padding: 1px 6px; }
@media (prefers-reduced-motion: reduce) { .bar-fill { transition: none; animation: none; } .live .dot { animation: none; } }
`

// contentHTML is the inner fragment served at /content and embedded in both
// the dashboard shell and the static snapshot.
const contentHTML = `
<div class="stats">
  <div class="stat"><div class="label">Total actions taken</div><div class="value">{{.Total}}</div></div>
  <div class="stat"><div class="label">Failing actions</div><div class="value">{{.Failing}}</div></div>
  <div class="stat"><div class="label">Health score</div><div class="value">{{.Score}}</div></div>
  <div class="stat"><div class="label">Status</div><div class="value" style="margin-top:2px"><span class="badge badge-{{.Status}}">{{.Status}}</span></div></div>
</div>

<div class="card">
  <h2>Flags Triggered</h2>
  {{if .HasFlags}}
  {{range .Flags}}<div class="flag">
    <div class="name"><span>{{.Name}}</span><span class="badge badge-critical">flag</span></div>
    <pre>{{.Evidence}}</pre>
  </div>{{end}}
  {{else}}<p class="empty">No flags triggered. Context looks healthy.</p>{{end}}
</div>

<div class="card">
  <h2>Churn: Net vs Gross</h2>
  <div class="bar-row">
    <div class="bar-meta"><span>Net</span><span class="val">{{printf "%.1f" .Churn.Net}}</span></div>
    <div class="bar-track"><div class="bar-fill bar-net" style="width:{{.Churn.NetPct}}%"></div></div>
  </div>
  <div class="bar-row">
    <div class="bar-meta"><span>Gross</span><span class="val">{{printf "%.1f" .Churn.Gross}}</span></div>
    <div class="bar-track"><div class="bar-fill bar-gross" style="width:{{.Churn.GrossPct}}%"></div></div>
  </div>
  <p class="muted" style="font-size:12.5px; margin:0">Efficiency (net/gross): <span class="mono">{{printf "%.2f" .Churn.Efficiency}}</span></p>
</div>

<div class="card">
  <h2>Error Loops</h2>
  {{if .Loop.Detected}}
  <table>
    <tr><th>Pairwise similarity</th><th>Value</th></tr>
    {{range .Loop.Similarities}}<tr><td>Jaccard similarity</td><td class="mono">{{printf "%.2f" .}}</td></tr>{{end}}
  </table>
  {{if .Loop.SharedTokens}}<div class="chips">{{range .Loop.SharedTokens}}<span class="chip">{{.}}</span>{{end}}</div>{{end}}
  {{else}}<p class="empty">No recurring error loop detected.</p>{{end}}
</div>

<div class="card">
  <h2>Recent Activity</h2>
  <table>
    <tr><th>Time</th><th>Kind</th><th>Command</th><th>Exit</th></tr>
    {{range .Recent}}<tr><td class="muted">{{.Time}}</td><td>{{.Kind}}</td><td class="mono">{{.Command}}</td><td class="{{if eq .ExitCode 0}}exit-ok{{else}}exit-bad{{end}}">{{.ExitCode}}</td></tr>
    {{else}}<tr><td colspan="4" class="empty">No activity recorded yet.</td></tr>{{end}}
  </table>
</div>
`

// dashboardHTML is the live shell served at "/": the <head> carries the
// HTMX script and the shared CSS; the main content area is the HTMX polling
// div, pre-filled with the current content for an instant first paint.
const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Vault — Live Dashboard</title>
<script src="https://unpkg.com/htmx.org@1.9.12"></script>
<style>{{template "css"}}</style>
</head>
<body>
<div class="page">
  <header class="masthead">
    <div>
      <div class="brand">Vault<span class="accent">.</span></div>
      <div class="muted" style="font-size:13px">Agent context-rot detection — live</div>
    </div>
    <div class="live"><span class="dot"></span> Live</div>
  </header>
  <main id="main-content" hx-get="/content" hx-trigger="every 1s" hx-swap="innerHTML">
{{template "content" .}}
  </main>
  <footer class="foot">
    <span>Updated every 1s via HTMX</span>
    <span>VAULT</span>
  </footer>
</div>
</body>
</html>
`

// staticHTML is the self-contained snapshot written by Generate: same shell
// and styling as the dashboard but with no HTMX script and no hx attributes,
// so the file works offline with zero external assets.
const staticHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Vault — Activity Report</title>
<style>{{template "css"}}</style>
</head>
<body>
<div class="page">
  <header class="masthead">
    <div>
      <div class="brand">Vault<span class="accent">.</span></div>
      <div class="muted" style="font-size:13px">Agent context-rot detection — static snapshot</div>
    </div>
  </header>
  <main id="main-content">
{{template "content" .}}
  </main>
  <footer class="foot">
    <span>Static snapshot — run <code>vault serve</code> for the live dashboard</span>
    <span>VAULT</span>
  </footer>
</div>
</body>
</html>
`
