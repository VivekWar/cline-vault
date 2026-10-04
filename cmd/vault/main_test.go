package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEndToEnd builds the real binary into t.TempDir() (make verify runs
// test BEFORE build, so bin/vault does not exist yet), pipes an initialize
// plus a tools/list request into it, and asserts stdout contains only valid
// JSON lines.
func TestEndToEnd(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "vault")
	root := t.TempDir() // VAULT_ROOT

	modRoot := findModuleRoot(t)
	build := exec.Command("go", "build", "-o", bin, "./cmd/vault")
	build.Dir = modRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n"
	proc := exec.Command(bin)
	proc.Env = append(os.Environ(), "VAULT_ROOT="+root)
	proc.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	if err := proc.Run(); err != nil {
		t.Fatalf("vault exited with error: %v\nstderr: %s", err, stderr.String())
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d stdout lines, want exactly 2: %q", len(lines), stdout.String())
	}
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("stdout line %d is not valid JSON: %v (%q)", i, err, line)
			continue
		}
		if m["jsonrpc"] != "2.0" {
			t.Errorf("stdout line %d jsonrpc = %v, want \"2.0\"", i, m["jsonrpc"])
		}
	}

	// The second line must be tools/list with exactly 4 tools.
	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	if len(list.Result.Tools) != 4 {
		t.Errorf("tools/list returned %d tools, want 4", len(list.Result.Tools))
	}
}

// findModuleRoot walks up from the working directory until it finds go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from cwd")
		}
		dir = parent
	}
}

// TestHealthCLI builds the binary and asserts `vault health --root <tmp>`
// prints a valid Health JSON object to stdout and exits 0.
func TestHealthCLI(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "vault")
	modRoot := findModuleRoot(t)
	build := exec.Command("go", "build", "-o", bin, "./cmd/vault")
	build.Dir = modRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	proc := exec.Command(bin, "health", "--root", t.TempDir())
	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	if err := proc.Run(); err != nil {
		t.Fatalf("vault health exited with error: %v\nstderr: %s", err, stderr.String())
	}

	var h struct {
		Score  int    `json:"score"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &h); err != nil {
		t.Fatalf("stdout is not valid JSON: %v (%s)", err, stdout.String())
	}
	if h.Status == "" {
		t.Errorf("missing status in %s", stdout.String())
	}
	if h.Score != 100 {
		t.Errorf("empty repo score = %d, want 100 (%s)", h.Score, stdout.String())
	}
}

// TestReportCLI builds the binary and asserts `vault report --root <tmp>
// '<json>'` routes the payload through report_activity: it prints the tool
// result and appends the activity line (with the git snapshot attempted).
func TestReportCLI(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "vault")
	modRoot := findModuleRoot(t)
	build := exec.Command("go", "build", "-o", bin, "./cmd/vault")
	build.Dir = modRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	t.Run("valid payload records", func(t *testing.T) {
		root := t.TempDir()
		proc := exec.Command(bin, "report", "--root", root,
			`{"kind":"EDIT","command":"edited a.go","exit_code":0,"files":["a.go"]}`)
		var stdout, stderr bytes.Buffer
		proc.Stdout = &stdout
		proc.Stderr = &stderr
		if err := proc.Run(); err != nil {
			t.Fatalf("vault report exited with error: %v\nstderr: %s", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), "recorded #1") {
			t.Errorf("stdout = %q, want 'recorded #1'", stdout.String())
		}
		data, err := os.ReadFile(filepath.Join(root, ".vault", "activity.jsonl"))
		if err != nil {
			t.Fatalf("read activity.jsonl: %v", err)
		}
		if !strings.Contains(string(data), `"kind":"EDIT"`) || !strings.Contains(string(data), "a.go") {
			t.Errorf("activity line wrong: %s", data)
		}
	})

	t.Run("invalid kind exits non-zero", func(t *testing.T) {
		proc := exec.Command(bin, "report", "--root", t.TempDir(), `{"kind":"BOGUS"}`)
		var stderr bytes.Buffer
		proc.Stderr = &stderr
		if err := proc.Run(); err == nil {
			t.Fatal("want non-zero exit for invalid kind")
		}
	})
}

// TestReportHTMLCLI builds the binary, seeds activity through the telemetry
// form of `vault report '<json>'`, then runs the bare form and asserts it
// writes a self-contained .vault/report.html.
func TestReportHTMLCLI(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "vault")
	modRoot := findModuleRoot(t)
	build := exec.Command("go", "build", "-o", bin, "./cmd/vault")
	build.Dir = modRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	root := t.TempDir()
	fail := `{"kind":"TEST","command":"go test","exit_code":1,` +
		`"stderr":"assertion mismatch: expected true but got false in calc_test.go","files":["calc_test.go"]}`
	for i := 0; i < 3; i++ {
		proc := exec.Command(bin, "report", "--root", root, fail)
		var stdout, stderr bytes.Buffer
		proc.Stdout = &stdout
		proc.Stderr = &stderr
		if err := proc.Run(); err != nil {
			t.Fatalf("seeding report failed: %v\nstderr: %s", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), "recorded #") {
			t.Fatalf("seed stdout = %q, want 'recorded #N'", stdout.String())
		}
	}

	proc := exec.Command(bin, "report", "--root", root)
	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	if err := proc.Run(); err != nil {
		t.Fatalf("vault report (html) exited with error: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote "+filepath.Join(root, ".vault", "report.html")) {
		t.Errorf("stdout = %q, want 'wrote <path>'", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(root, ".vault", "report.html"))
	if err != nil {
		t.Fatalf("read report.html: %v", err)
	}
	html := string(data)
	for _, want := range []string{"<!DOCTYPE html>", "RECURRING_ERROR_LOOP", "Total actions taken"} {
		if !strings.Contains(html, want) {
			t.Errorf("report.html missing %q", want)
		}
	}
}
