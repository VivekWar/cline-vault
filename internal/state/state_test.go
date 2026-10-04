package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
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
	text, isErr := st.createHandoff(json.RawMessage(args))
	if isErr {
		t.Fatalf("createHandoff failed: %s", text)
	}
	wantPath := filepath.Join(st.vault, "handoff_state.md")
	if !strings.HasPrefix(text, wantPath) {
		t.Errorf("result = %q, want prefix %q", text, wantPath)
	}
	if !filepath.IsAbs(wantPath) {
		t.Errorf("handoff path not absolute: %q", wantPath)
	}

	data, err := os.ReadFile(wantPath)
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

// TestCreateHandoffUsesWorkspaceGit: when workspace is an absolute git repo,
// the git section reflects that directory (branch + commit), not VAULT_ROOT.
func TestCreateHandoffUsesWorkspaceGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ws := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = ws
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "f.txt")
	git("commit", "-q", "-m", "c1")

	short := gitOut(t, ws, "rev-parse", "--short", "HEAD")

	st := New(t.TempDir()) // VAULT_ROOT is a separate, non-repo temp dir
	text, isErr := st.createHandoff(json.RawMessage(`{"goal":"G","next_action":"NA","workspace":"` + ws + `"}`))
	if isErr {
		t.Fatalf("createHandoff: %s", text)
	}
	content := readHandoffFile(t, st)
	if !strings.Contains(content, "workspace: "+ws) {
		t.Errorf("git section missing workspace path:\n%s", content)
	}
	if !strings.Contains(content, "branch: main") {
		t.Errorf("git section missing branch:\n%s", content)
	}
	if !strings.Contains(content, "commit: "+short) {
		t.Errorf("git section missing commit %s:\n%s", short, content)
	}
}

// TestGitDirFallback: non-absolute or non-git workspaces fall back to root.
func TestGitDirFallback(t *testing.T) {
	st := New(t.TempDir())
	st.log = log.New(io.Discard, "", 0)
	for _, ws := range []string{"", "relative/path", "/this/does/not/exist"} {
		if got := st.gitDir(ws); got != st.root {
			t.Errorf("gitDir(%q) = %q, want VAULT_ROOT %q", ws, got, st.root)
		}
	}
	// A relative workspace must not error the whole handoff.
	text, isErr := st.createHandoff(json.RawMessage(`{"goal":"G","next_action":"NA","workspace":"relative/path"}`))
	if isErr {
		t.Fatalf("createHandoff with relative workspace must not error: %s", text)
	}
}

// TestReadHandoff: error before any handoff, content after one is created.
func TestReadHandoff(t *testing.T) {
	st := New(t.TempDir())
	text, isErr := st.readHandoff()
	if !isErr {
		t.Fatalf("readHandoff before handoff: want isError, got %q", text)
	}
	if text != "no handoff yet" {
		t.Errorf("got %q, want 'no handoff yet'", text)
	}
	if _, isErr := st.createHandoff(json.RawMessage(`{"goal":"G","next_action":"NA"}`)); isErr {
		t.Fatal("createHandoff failed")
	}
	text, isErr = st.readHandoff()
	if isErr {
		t.Fatalf("readHandoff after handoff: unexpected error %q", text)
	}
	if !strings.HasPrefix(text, "# Vault Handoff\n") {
		t.Errorf("readHandoff missing title: %q", text)
	}
}

// TestActivityRotation: after create_handoff the activity log is archived and
// the next report_activity starts back at #1; with no activity, rotation is
// skipped and reported as "none".
func TestActivityRotation(t *testing.T) {
	t.Run("with activity", func(t *testing.T) {
		st := New(t.TempDir())
		mustReport(t, st, `{"kind":"COMMAND","command":"a","exit_code":0,"files":["a.go"]}`)
		mustReport(t, st, `{"kind":"COMMAND","command":"b","exit_code":0,"files":["b.go"]}`)
		text, isErr := st.createHandoff(json.RawMessage(`{"goal":"G","next_action":"NA"}`))
		if isErr {
			t.Fatalf("createHandoff: %s", text)
		}
		if !strings.Contains(text, "activity archived to: ") || strings.Contains(text, "activity archived to: none") {
			t.Errorf("result missing a real archive path: %s", text)
		}
		matches, _ := filepath.Glob(filepath.Join(st.vault, "archive", "activity-*.jsonl"))
		if len(matches) != 1 {
			t.Fatalf("archive files = %d, want 1", len(matches))
		}
		if got := mustReport(t, st, `{"kind":"COMMAND","command":"c","exit_code":0,"files":["c.go"]}`); got != "recorded #1" {
			t.Errorf("after rotation got %q, want 'recorded #1'", got)
		}
	})
	t.Run("no activity", func(t *testing.T) {
		st := New(t.TempDir())
		text, isErr := st.createHandoff(json.RawMessage(`{"goal":"G","next_action":"NA"}`))
		if isErr {
			t.Fatalf("createHandoff: %s", text)
		}
		if !strings.Contains(text, "activity archived to: none") {
			t.Errorf("result missing 'none' for empty activity: %s", text)
		}
		if _, err := os.Stat(filepath.Join(st.vault, "archive")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("archive dir must not be created when nothing to archive (err=%v)", err)
		}
	})
}

// TestHandoffTitleAndEmptySections: title + resume line first, empty sections
// render "_none_".
func TestHandoffTitleAndEmptySections(t *testing.T) {
	st := New(t.TempDir())
	if _, isErr := st.createHandoff(json.RawMessage(`{"goal":"G","next_action":"NA"}`)); isErr {
		t.Fatal("createHandoff failed")
	}
	content := readHandoffFile(t, st)
	wantPrefix := "# Vault Handoff\nResume: continue from Next Immediate Action. Do NOT repeat anything in Failed Attempts.\n"
	if !strings.HasPrefix(content, wantPrefix) {
		t.Errorf("title/resume line not first:\n%s", content)
	}
	for _, sec := range []string{
		"## Architecture & Decisions\n_none_",
		"## Completed Work\n_none_",
		"## Current Blocker & Active Errors\n_none_",
		"## Failed Attempts (Do Not Repeat)\n_none_",
		"## Touched Files & AST Scope\n_none_\nAST scope: not computed (paths only)",
	} {
		if !strings.Contains(content, sec) {
			t.Errorf("missing empty section %q:\n%s", sec, content)
		}
	}
}

// TestReportActivityStoresWorkspace: the workspace arg is persisted per entry.
func TestReportActivityStoresWorkspace(t *testing.T) {
	st := New(t.TempDir())
	mustReport(t, st, `{"kind":"COMMAND","command":"c","exit_code":0,"files":["x.go"],"workspace":"/tmp/ws"}`)
	data, err := os.ReadFile(filepath.Join(st.vault, "activity.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"workspace":"/tmp/ws"`) {
		t.Errorf("workspace not stored in activity line: %s", data)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func readHandoffFile(t *testing.T, st *State) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(st.vault, "handoff_state.md"))
	if err != nil {
		t.Fatalf("read handoff: %v", err)
	}
	return string(data)
}

// TestReportActivityRedactsStderr: secrets in the stderr argument must be
// masked before the activity line is appended to activity.jsonl.
func TestReportActivityRedactsStderr(t *testing.T) {
	st := New(t.TempDir())
	mustReport(t, st, `{"kind":"COMMAND","command":"c","exit_code":1,"stderr":"token: sk-abcdefghijklmnopqrstuvwxyz","files":[]}`)
	data, err := os.ReadFile(filepath.Join(st.vault, "activity.jsonl"))
	if err != nil {
		t.Fatalf("read activity.jsonl: %v", err)
	}
	raw := string(data)
	if !strings.Contains(raw, "[REDACTED]") {
		t.Errorf("activity line missing [REDACTED]: %s", raw)
	}
	if strings.Contains(raw, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("raw API key persisted in activity log: %s", raw)
	}
}

// TestReportActivitySkipsSnapshotForRead: READ activities must not trigger a
// git snapshot (no `tree` field), while other kinds still record the tree
// hash of the workspace.
func TestReportActivitySkipsSnapshotForRead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ws := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = ws
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "f.txt")
	git("commit", "-q", "-m", "base")

	st := New(t.TempDir())
	mustReport(t, st, `{"kind":"READ","command":"cat f.txt","exit_code":0,"files":["f.txt"],"workspace":"`+ws+`"}`)
	mustReport(t, st, `{"kind":"EDIT","command":"edited f.txt","exit_code":0,"files":["f.txt"],"workspace":"`+ws+`"}`)

	data, err := os.ReadFile(filepath.Join(st.vault, "activity.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d activity lines, want 2: %s", len(lines), data)
	}
	if strings.Contains(lines[0], `"tree"`) {
		t.Errorf("READ activity must not carry a tree field: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"tree"`) {
		t.Errorf("EDIT activity must carry a tree field: %s", lines[1])
	}
}
