package report

import (
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

// TestGenerateWithErrorLoop seeds three failing TEST entries with identical
// output and asserts the report carries the total count and the
// RECURRING_ERROR_LOOP flag.
func TestGenerateWithErrorLoop(t *testing.T) {
	root := t.TempDir()
	fail := `{"time":"2026-10-04T10:00:00Z","kind":"TEST","command":"go test","exit_code":1,"stderr":"assertion mismatch: expected true but got false in calc_test.go","files":["calc_test.go"],"workspace":"","tree":""}`
	seed(t, root, strings.Join([]string{fail, fail, fail}, "\n")+"\n")

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
		"Vault Activity Report",
		"RECURRING_ERROR_LOOP",
		"Total actions taken",
		"Churn: Net vs Gross",
		"Error Loops",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report.html missing %q", want)
		}
	}
	if !strings.Contains(html, ">3</span>") {
		t.Errorf("total actions not rendered as 3:\n%s", html)
	}
	// No external assets: every asset must be inline.
	for _, external := range []string{"http://", "https://", "<link"} {
		if strings.Contains(html, external) {
			t.Errorf("report.html references external asset %q", external)
		}
	}
}

// TestGenerateEmpty: a missing activity log still produces a valid report
// with zero totals and no flags.
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

// TestBuildPageChurnBars: net/gross bar widths are percentages of the larger
// value and stay 0 when both are 0.
func TestBuildPageChurnBars(t *testing.T) {
	data := buildPage(nil, healthWithChurn(30, 60), 0)
	if data.Churn.NetPct != 50 || data.Churn.GrossPct != 100 {
		t.Errorf("bars = %d/%d, want 50/100", data.Churn.NetPct, data.Churn.GrossPct)
	}
	zero := buildPage(nil, healthWithChurn(0, 0), 0)
	if zero.Churn.NetPct != 0 || zero.Churn.GrossPct != 0 {
		t.Errorf("zero churn bars = %d/%d, want 0/0", zero.Churn.NetPct, zero.Churn.GrossPct)
	}
}

// TestBuildPageRedactsCommands: secrets in the Command field must never reach
// the report's recent-activity table.
func TestBuildPageRedactsCommands(t *testing.T) {
	acts := []activityLine{{
		Time:     "2026-10-04T10:00:00Z",
		Kind:     "COMMAND",
		Command:  "export API_KEY=sk-abcdefghijklmnopqrstuvwxyz",
		ExitCode: 0,
	}}
	data := buildPage(acts, healthWithChurn(0, 0), 0)
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
