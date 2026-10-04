package report

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vault/internal/heuristics"
)

// seed writes raw JSONL lines into root/.vault/activity.jsonl.
func seed(t *testing.T, root, jsonl string) {
	t.Helper()
	vault := filepath.Join(root, ".vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "activity.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}
}

// appendLines appends raw JSONL lines to root/.vault/activity.jsonl.
func appendLines(t *testing.T, root, jsonl string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(root, ".vault", "activity.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(jsonl); err != nil {
		t.Fatal(err)
	}
}

// get renders a handler for GET /path and returns the response body.
func get(t *testing.T, h http.HandlerFunc, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	h(rec, req)
	return rec.Body.String()
}

// failLine is one failing TEST entry whose identical triple triggers
// RECURRING_ERROR_LOOP.
const failLine = `{"time":"2026-10-04T10:00:00Z","kind":"TEST","command":"go test","exit_code":1,"stderr":"assertion mismatch: expected true but got false in calc_test.go","files":["calc_test.go"],"workspace":"","tree":""}`

// TestGenerateWithErrorLoop seeds three failing TEST entries with identical
// output and asserts the static snapshot carries the total count, the
// RECURRING_ERROR_LOOP flag and the polished sections — with zero external
// assets (the static file must stay fully self-contained).
func TestGenerateWithErrorLoop(t *testing.T) {
	root := t.TempDir()
	seed(t, root, strings.Join([]string{failLine, failLine, failLine}, "\n")+"\n")

	path, err := Generate(root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if path != filepath.Join(root, ".vault", "report.html") {
		t.Errorf("path = %q, want <root>/.vault/report.html", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report.html: %v", err)
	}
	html := string(data)
	for _, want := range []string{
		"<!DOCTYPE html>",
		"Vault — Activity Report",
		"RECURRING_ERROR_LOOP",
		"Total actions taken",
		"Churn: Net vs Gross",
		"Error Loops",
		"Recent Activity",
		"badge-critical",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report.html missing %q", want)
		}
	}
	if !strings.Contains(html, `>3</div>`) {
		t.Errorf("total actions not rendered as 3:\n%s", html)
	}
	// The static snapshot has no HTMX and no external assets.
	for _, external := range []string{"http://", "https://", "<link", "hx-", "htmx"} {
		if strings.Contains(html, external) {
			t.Errorf("static report.html references %q — must stay self-contained", external)
		}
	}
}

// TestGenerateEmpty: a missing activity log still produces a valid static
// snapshot with zero totals and no flags.
func TestGenerateEmpty(t *testing.T) {
	root := t.TempDir()
	path, err := Generate(root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report.html: %v", err)
	}
	html := string(data)
	for _, want := range []string{"No flags triggered.", "No activity recorded yet.", "No recurring error loop detected."} {
		if !strings.Contains(html, want) {
			t.Errorf("empty report missing %q", want)
		}
	}
}

// TestDashboardHandler: "/" serves the full shell — the exact HTMX script in
// the head, the polling div with the exact hx attributes, and the content
// pre-filled so the first paint is instant.
func TestDashboardHandler(t *testing.T) {
	root := t.TempDir()
	seed(t, root, failLine+"\n")
	html := get(t, DashboardHandler(root), "/")
	for _, want := range []string{
		"<!DOCTYPE html>",
		`<script src="/htmx.min.js"></script>`,
		`<main id="main-content" hx-get="/content" hx-trigger="every 1s" hx-swap="innerHTML">`,
		"Total actions taken",
		"transition: width 0.3s ease",
		"Vault — Live Dashboard",
		`data-kind="all"`,
		`setInterval(refresh, 1000)`,
		`htmx.ajax("GET", url, { target: "#main-content", swap: "innerHTML" })`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	if !strings.Contains(html, `>1</div>`) {
		t.Errorf("dashboard must pre-fill content for the first paint:\n%s", html)
	}
}

// TestContentHandler: "/content" serves ONLY the inner fragment — no shell,
// no script, no styles — and reflects the current activity.
func TestContentHandler(t *testing.T) {
	root := t.TempDir()
	seed(t, root, strings.Join([]string{failLine, failLine, failLine}, "\n")+"\n")
	html := get(t, ContentHandler(root), "/content")
	for _, want := range []string{"RECURRING_ERROR_LOOP", "Total actions taken", "Churn: Net vs Gross"} {
		if !strings.Contains(html, want) {
			t.Errorf("content missing %q", want)
		}
	}
	for _, forbidden := range []string{"<!DOCTYPE", "<html", "<head", "<script", "<style", "<body"} {
		if strings.Contains(html, forbidden) {
			t.Errorf("/content must be an inner fragment only, found %q", forbidden)
		}
	}
	if !strings.Contains(html, `>3</div>`) {
		t.Errorf("content total = want 3:\n%s", html)
	}
}

// TestContentLiveUpdates: the fragment reflects new activity on the very
// next request — the property HTMX's 1s polling relies on.
func TestContentLiveUpdates(t *testing.T) {
	root := t.TempDir()
	seed(t, root, failLine+"\n")
	h := ContentHandler(root)
	if got := get(t, h, "/content"); !strings.Contains(got, `>1</div>`) {
		t.Fatalf("first read total != 1:\n%s", got)
	}
	appendLines(t, root, failLine+"\n")
	if got := get(t, h, "/content"); !strings.Contains(got, `>2</div>`) {
		t.Fatalf("second read total != 2 (live update failed):\n%s", got)
	}
}

// TestContentKindFilter: ?kind= filters the recent-activity table and marks
// the matching chip active, while the totals stay global.
func TestContentKindFilter(t *testing.T) {
	root := t.TempDir()
	seed(t, root, strings.Join([]string{
		`{"time":"2026-10-04T10:00:00Z","kind":"COMMAND","command":"cmd1","exit_code":0,"stderr":"","files":[],"workspace":""}`,
		`{"time":"2026-10-04T10:00:01Z","kind":"COMMAND","command":"cmd2","exit_code":0,"stderr":"","files":[],"workspace":""}`,
		`{"time":"2026-10-04T10:00:02Z","kind":"TEST","command":"go test","exit_code":1,"stderr":"boom","files":[],"workspace":""}`,
	}, "\n")+"\n")

	frag := get(t, ContentHandler(root), "/content?kind=COMMAND")
	if !strings.Contains(frag, `data-kind="COMMAND">COMMAND<em>2</em>`) {
		t.Errorf("COMMAND chip missing its count:\n%s", frag)
	}
	if !strings.Contains(frag, `class="chip active" data-kind="COMMAND"`) {
		t.Errorf("COMMAND chip must be active when filtered:\n%s", frag)
	}
	if strings.Contains(frag, "go test") {
		t.Errorf("TEST rows must be filtered out for kind=COMMAND:\n%s", frag)
	}
	if !strings.Contains(frag, "cmd1") || !strings.Contains(frag, "cmd2") {
		t.Errorf("COMMAND rows missing:\n%s", frag)
	}
	if !strings.Contains(frag, ">3</div>") {
		t.Errorf("total must stay global (3) under a filter:\n%s", frag)
	}

	// Unknown kinds fall back to "all".
	all := get(t, ContentHandler(root), "/content?kind=BOGUS")
	if !strings.Contains(all, "go test") || !strings.Contains(all, "cmd1") {
		t.Errorf("unknown kind must fall back to all:\n%s", all)
	}
}

// TestHTMXHandler: /htmx.min.js serves the vendored HTMX script locally.
func TestHTMXHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/htmx.min.js", nil)
	HTMXHandler()(rec, req)
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/javascript") {
		t.Errorf("content-type = %q, want application/javascript", ct)
	}
	if !strings.Contains(rec.Body.String(), "htmx") {
		t.Errorf("served file does not look like htmx (%d bytes)", rec.Body.Len())
	}
}

// TestBuildPageChurnBars: net/gross bar widths are percentages of the larger
// value and stay 0 when both are 0.
func TestBuildPageChurnBars(t *testing.T) {
	data := buildPage(nil, healthWithChurn(30, 60), 0, "all")
	if data.Churn.NetPct != 50 || data.Churn.GrossPct != 100 {
		t.Errorf("bars = %d/%d, want 50/100", data.Churn.NetPct, data.Churn.GrossPct)
	}
	if !data.Churn.HasChurn {
		t.Error("HasChurn must be true when churn is non-zero")
	}
	zero := buildPage(nil, healthWithChurn(0, 0), 0, "all")
	if zero.Churn.NetPct != 0 || zero.Churn.GrossPct != 0 {
		t.Errorf("zero churn bars = %d/%d, want 0/0", zero.Churn.NetPct, zero.Churn.GrossPct)
	}
	if zero.Churn.HasChurn {
		t.Error("HasChurn must be false when churn is zero")
	}
}

// TestBuildPageRedactsCommands: secrets in the Command field must never reach
// the recent-activity table.
func TestBuildPageRedactsCommands(t *testing.T) {
	acts := []activityLine{{
		Time:     "2026-10-04T10:00:00Z",
		Kind:     "COMMAND",
		Command:  "export API_KEY=sk-abcdefghijklmnopqrstuvwxyz",
		ExitCode: 0,
	}}
	data := buildPage(acts, healthWithChurn(0, 0), 0, "all")
	if len(data.Recent) != 1 {
		t.Fatalf("recent rows = %d, want 1", len(data.Recent))
	}
	if strings.Contains(data.Recent[0].Command, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("raw API key in recent command: %q", data.Recent[0].Command)
	}
	if !strings.Contains(data.Recent[0].Command, "[REDACTED]") {
		t.Errorf("command not redacted: %q", data.Recent[0].Command)
	}
}

// healthWithChurn builds a Health value carrying the given churn metrics.
func healthWithChurn(net, gross float64) heuristics.Health {
	return heuristics.Health{
		Score:   100,
		Status:  "healthy",
		Reason:  "no context-rot signals detected.",
		Flags:   []heuristics.Flag{},
		Metrics: heuristics.Metrics{Net: net, Gross: gross, Efficiency: 0},
	}
}
