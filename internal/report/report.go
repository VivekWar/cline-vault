// Package report generates a self-contained HTML report from Vault's activity
// log: it reads root/.vault/activity.jsonl, runs the same heuristics.Assess
// aggregation that check_context_health uses, and renders root/.vault/
// report.html with html/template and inline CSS (no external assets).
package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
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

// pageData is the template data for report.html.
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
// root/.vault/report.html (inline CSS, no external assets). It returns the
// absolute path written. A missing activity log yields an empty report.
func Generate(root string) (string, error) {
	acts, err := readActivities(filepath.Join(root, ".vault", "activity.jsonl"))
	if err != nil {
		return "", err
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
	data := buildPage(acts, health, failing)

	var buf bytes.Buffer
	if err := pageTmpl.Execute(&buf, data); err != nil {
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

// pageTmpl renders the self-contained HTML report (inline CSS only).
var pageTmpl = template.Must(template.New("report").Parse(pageHTML))

const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Vault Activity Report</title>
<style>
  body { font-family: system-ui, -apple-system, "Segoe UI", sans-serif; margin: 2rem auto; max-width: 62rem; padding: 0 1rem; color: #1f2937; background: #f9fafb; }
  h1 { border-bottom: 2px solid #e5e7eb; padding-bottom: .5rem; }
  h2 { margin-top: 2rem; }
  .card { background: #fff; border: 1px solid #e5e7eb; border-radius: .5rem; padding: 1rem 1.25rem; margin: 1rem 0; }
  .stat { display: inline-block; min-width: 9rem; margin: .25rem .5rem .25rem 0; }
  .stat .value { font-size: 1.75rem; font-weight: 700; }
  .healthy { color: #15803d; } .degraded { color: #b45309; } .critical { color: #b91c1c; }
  table { border-collapse: collapse; width: 100%; }
  th, td { border: 1px solid #e5e7eb; padding: .4rem .6rem; text-align: left; font-size: .9rem; }
  th { background: #f3f4f6; }
  .flag { border-left: 4px solid #b91c1c; background: #fef2f2; }
  .bar-track { background: #e5e7eb; border-radius: .25rem; height: 1.2rem; margin: .5rem 0; overflow: hidden; }
  .bar-net { background: #2563eb; height: 100%; }
  .bar-gross { background: #9333ea; height: 100%; }
  .muted { color: #6b7280; font-size: .85rem; }
  code { background: #f3f4f6; border-radius: .25rem; padding: .1rem .3rem; }
</style>
</head>
<body>
<h1>Vault Activity Report</h1>
<p class="muted">Generated from <code>.vault/activity.jsonl</code> — self-contained, no external assets.</p>

<h2>Summary</h2>
<div class="card">
  <span class="stat"><span class="value">{{.Total}}</span><br>Total actions taken</span>
  <span class="stat"><span class="value">{{.Failing}}</span><br>Failing actions</span>
  <span class="stat"><span class="value">{{.Score}}</span><br>Health score</span>
  <span class="stat"><span class="value {{.Status}}">{{.Status}}</span><br>Status</span>
  <p>{{.Reason}}</p>
</div>

<h2>Flags Triggered</h2>
{{if .HasFlags}}
{{range .Flags}}<div class="card flag">
  <strong>{{.Name}}</strong>
  <pre class="muted">{{.Evidence}}</pre>
</div>
{{end}}
{{else}}
<p class="muted">No flags triggered.</p>
{{end}}

<h2>Churn: Net vs Gross</h2>
<div class="card">
  <table>
    <tr><th>Metric</th><th>Value</th></tr>
    <tr><td>Net churn (lines)</td><td>{{printf "%.1f" .Churn.Net}}</td></tr>
    <tr><td>Gross churn (lines)</td><td>{{printf "%.1f" .Churn.Gross}}</td></tr>
    <tr><td>Efficiency (net/gross)</td><td>{{printf "%.2f" .Churn.Efficiency}}</td></tr>
  </table>
  <p class="muted">Net</p>
  <div class="bar-track"><div class="bar-net" style="width: {{.Churn.NetPct}}%"></div></div>
  <p class="muted">Gross</p>
  <div class="bar-track"><div class="bar-gross" style="width: {{.Churn.GrossPct}}%"></div></div>
</div>

<h2>Error Loops</h2>
{{if .Loop.Detected}}
<div class="card">
  <table>
    <tr><th>Pairwise output similarity</th><th>Value</th></tr>
    {{range .Loop.Similarities}}<tr><td>Jaccard similarity</td><td>{{printf "%.2f" .}}</td></tr>{{end}}
  </table>
  <p class="muted">Shared tokens across the last 3 failing outputs:
    {{range .Loop.SharedTokens}}<code>{{.}}</code> {{end}}</p>
</div>
{{else}}
<p class="muted">No recurring error loop detected.</p>
{{end}}

<h2>Recent Activity</h2>
<div class="card">
  <table>
    <tr><th>Time</th><th>Kind</th><th>Command</th><th>Exit</th></tr>
    {{range .Recent}}<tr><td>{{.Time}}</td><td>{{.Kind}}</td><td><code>{{.Command}}</code></td><td>{{.ExitCode}}</td></tr>
    {{else}}<tr><td colspan="4" class="muted">No activity recorded yet.</td></tr>{{end}}
  </table>
</div>
</body>
</html>
`
