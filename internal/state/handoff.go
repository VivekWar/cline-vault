package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// createHandoffArgs is the tools/call arguments for create_handoff.
type createHandoffArgs struct {
	Goal           string   `json:"goal"`
	Decisions      string   `json:"decisions"`
	CompletedWork  string   `json:"completed_work"`
	Blocker        string   `json:"blocker"`
	FailedAttempts []string `json:"failed_attempts"`
	NextAction     string   `json:"next_action"`
}

// createHandoff writes root/.vault/handoff_state.md atomically and returns
// its absolute path.
func (s *State) createHandoff(argsJSON json.RawMessage) (string, bool) {
	var args createHandoffArgs
	if len(argsJSON) > 0 && string(argsJSON) != "null" {
		if err := json.Unmarshal(argsJSON, &args); err != nil {
			return fmt.Sprintf("invalid arguments: %v", err), true
		}
	}
	if args.Goal == "" {
		return "missing required argument: goal", true
	}
	if args.NextAction == "" {
		return "missing required argument: next_action", true
	}
	if err := s.ensureVault(); err != nil {
		return fmt.Sprintf("cannot create state dir: %v", err), true
	}
	activities, err := s.readActivities()
	if err != nil {
		return fmt.Sprintf("cannot read activity log: %v", err), true
	}
	path := filepath.Join(s.vault, "handoff_state.md")
	if err := atomicWrite(path, buildHandoff(args, activities, s.gitState())); err != nil {
		return fmt.Sprintf("cannot write handoff: %v", err), true
	}
	return path, false
}

// readActivities parses activity.jsonl, skipping unparseable lines.
func (s *State) readActivities() ([]activityEntry, error) {
	data, err := os.ReadFile(s.activityPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []activityEntry
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e activityEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // tolerate corrupt lines
		}
		out = append(out, e)
	}
	return out, nil
}

// buildHandoff renders the handoff state file with the exact H2 section order.
func buildHandoff(args createHandoffArgs, acts []activityEntry, git string) string {
	var b strings.Builder
	b.WriteString("## Goal\n")
	b.WriteString(args.Goal)
	b.WriteString("\n\n## Architecture & Decisions\n")
	b.WriteString(args.Decisions)
	b.WriteString("\n\n## Touched Files & AST Scope\n")
	for _, f := range dedupFiles(acts) {
		b.WriteString(f)
		b.WriteByte('\n')
	}
	b.WriteString("AST scope: not computed (paths only)\n")
	b.WriteString("\n## Completed Work\n")
	b.WriteString(args.CompletedWork)
	b.WriteString("\n\n## Current Blocker & Active Errors\n")
	b.WriteString(args.Blocker)
	b.WriteByte('\n')
	b.WriteString(activeErrors(acts))
	b.WriteString("\n\n## Failed Attempts (Do Not Repeat)\n")
	for _, fa := range args.FailedAttempts {
		b.WriteString("- ")
		b.WriteString(fa)
		b.WriteByte('\n')
	}
	b.WriteString("\n## Next Immediate Action\n")
	b.WriteString(args.NextAction)
	b.WriteString("\n\n## Git State & Checkpoint Tag\n")
	b.WriteString(git)
	b.WriteByte('\n')
	return b.String()
}

// dedupFiles returns the de-duplicated union of files across activities in
// first-seen order.
func dedupFiles(acts []activityEntry) []string {
	seen := make(map[string]bool)
	var out []string
	for _, a := range acts {
		for _, f := range a.Files {
			if f != "" && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

// activeErrors renders the stderr of the last 3 failing (exit_code != 0)
// activities, truncated to 40 lines each.
func activeErrors(acts []activityEntry) string {
	var failing []activityEntry
	for _, a := range acts {
		if a.ExitCode != 0 {
			failing = append(failing, a)
		}
	}
	if len(failing) == 0 {
		return "Active errors: none"
	}
	if len(failing) > 3 {
		failing = failing[len(failing)-3:]
	}
	var b strings.Builder
	for i, a := range failing {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(truncateLines(a.Stderr, 40))
	}
	return b.String()
}

// truncateLines keeps at most n lines of s.
func truncateLines(s string, n int) string {
	if s == "" {
		return "(empty stderr)"
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	lines = append(lines[:n], fmt.Sprintf("… (%d more lines truncated)", len(lines)-n))
	return strings.Join(lines, "\n")
}

// gitState returns the git commit and porcelain status of root, or
// "git unavailable" when git fails (never an error).
func (s *State) gitState() string {
	short, err1 := gitOutput(s.root, "rev-parse", "--short", "HEAD")
	status, err2 := gitOutput(s.root, "status", "--porcelain")
	if err1 != nil || err2 != nil {
		return "git unavailable"
	}
	return fmt.Sprintf("commit: %s\nstatus:\n%s", short, status)
}

// gitOutput runs git in dir and returns trimmed stdout.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// atomicWrite writes content to path via a temp file + rename.
func atomicWrite(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".vault-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { os.Remove(tmpName) }
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
