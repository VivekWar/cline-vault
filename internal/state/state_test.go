package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustReport seeds one activity entry via the real reportActivity path.
func mustReport(t *testing.T, st *State, args string) string {
	t.Helper()
	text, isErr := st.reportActivity(json.RawMessage(args))
	if isErr {
		t.Fatalf("reportActivity(%s) failed: %s", args, text)
	}
	return text
}

// TestReportActivityCounts asserts "recorded #N" increments and that .vault/
// is created on demand (not before the first write).
func TestReportActivityCounts(t *testing.T) {
	st := New(t.TempDir())

	// .vault must NOT exist before the first write (state creates it on demand).
	if _, err := os.Stat(st.vault); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".vault exists before any write (err=%v), want not-created-yet", err)
	}

	for i := 1; i <= 3; i++ {
		got := mustReport(t, st,
			fmt.Sprintf(`{"kind":"COMMAND","command":"cmd%d","exit_code":0,"files":["f%d.go"]}`, i, i))
		want := fmt.Sprintf("recorded #%d", i)
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}

	data, err := os.ReadFile(filepath.Join(st.vault, "activity.jsonl"))
	if err != nil {
		t.Fatalf("read activity.jsonl: %v", err)
	}
	if got := bytes.Count(data, []byte("\n")); got != 3 {
		t.Errorf("activity.jsonl has %d lines, want 3", got)
	}
}

// TestCreateHandoff asserts the handoff file contains all 8 H2 headings in
// order, de-duplicated touched files, stderr from failing entries, and
// "git unavailable" when root is not a git repo.
func TestCreateHandoff(t *testing.T) {
	st := New(t.TempDir())
	mustReport(t, st, `{"kind":"EDIT","command":"x","exit_code":0,"stderr":"","files":["a.go","b.go"]}`)
	mustReport(t, st, `{"kind":"TEST","command":"go test","exit_code":1,"stderr":"boom 1\nboom 2","files":["a.go","c.go"]}`)
	mustReport(t, st, `{"kind":"COMMIT","command":"git commit","exit_code":0,"stderr":"","files":["b.go","a.go","c.go"]}`)

	args := `{"goal":"G","decisions":"D","completed_work":"CW","blocker":"B",` +
		`"failed_attempts":["f1","f2"],"next_action":"NA"}`
	path, isErr := st.createHandoff(json.RawMessage(args))
	if isErr {
		t.Fatalf("createHandoff failed: %s", path)
	}
	wantPath := filepath.Join(st.vault, "handoff_state.md")
	if path != wantPath {
		t.Errorf("returned path = %q, want %q", path, wantPath)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("returned path is not absolute: %q", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read handoff: %v", err)
	}
	content := string(data)

	headings := []string{
		"## Goal",
		"## Architecture & Decisions",
		"## Touched Files & AST Scope",
		"## Completed Work",
		"## Current Blocker & Active Errors",
		"## Failed Attempts (Do Not Repeat)",
		"## Next Immediate Action",
		"## Git State & Checkpoint Tag",
	}
	last := -1
	for _, h := range headings {
		i := strings.Index(content, h)
		if i < 0 {
			t.Errorf("missing heading %q", h)
			continue
		}
		if i <= last {
			t.Errorf("heading %q out of order (at %d, previous at %d)", h, i, last)
		}
		last = i
	}

	// Touched files: de-duplicated union, paths only, plus the AST scope note.
	start := strings.Index(content, "## Touched Files & AST Scope")
	end := strings.Index(content, "## Completed Work")
	if start < 0 || end < 0 || start >= end {
		t.Fatalf("cannot isolate Touched Files section")
	}
	tf := content[start:end]
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		if got := strings.Count(tf, f); got != 1 {
			t.Errorf("touched file %q appears %d times, want exactly 1\n%s", f, got, tf)
		}
	}
	if !strings.Contains(tf, "AST scope: not computed (paths only)") {
		t.Errorf("missing AST scope note:\n%s", tf)
	}

	// Active errors carry stderr from failing entries.
	if !strings.Contains(content, "boom 1") {
		t.Errorf("handoff missing stderr from the failing activity")
	}

	// Root is a bare temp dir, not a git repo: must say "git unavailable" and
	// still succeed.
	if !strings.Contains(content, "git unavailable") {
		t.Errorf("handoff missing 'git unavailable' for a non-repo root")
	}
}

// TestCreateHandoffMissingGoal covers the tool-level validation error.
func TestCreateHandoffMissingGoal(t *testing.T) {
	st := New(t.TempDir())
	text, isErr := st.createHandoff(json.RawMessage(`{"next_action":"x"}`))
	if !isErr {
		t.Fatalf("want isError for missing goal, got: %s", text)
	}
	if !strings.Contains(text, "goal") {
		t.Errorf("error text should mention goal: %s", text)
	}
}
