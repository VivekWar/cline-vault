package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vault/internal/heuristics"
)

// createHandoffArgs is the tools/call arguments for create_handoff.
type createHandoffArgs struct {
	Goal           string   `json:"goal"`
	Decisions      string   `json:"decisions"`
	CompletedWork  string   `json:"completed_work"`
	Blocker        string   `json:"blocker"`
	FailedAttempts []string `json:"failed_attempts"`
	NextAction     string   `json:"next_action"`
	Workspace      string   `json:"workspace"`
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
	if err := atomicWrite(path, buildHandoff(args, activities, s.gitState(args.Workspace))); err != nil {
		return fmt.Sprintf("cannot write handoff: %v", err), true
	}
	// Rotate the activity log only after the handoff write has succeeded.
	archivePath, err := s.archiveActivity()
	if err != nil {
		return fmt.Sprintf("cannot archive activity log: %v", err), true
	}
	archived := "none"
	if archivePath != "" {
		archived = archivePath
	}
	return path + "\nactivity archived to: " + archived, false
}

// archiveActivity moves activity.jsonl into .vault/archive/ with a UTC
// timestamp and returns the new path. It returns ("", nil) when there is no
// activity to archive.
func (s *State) archiveActivity() (string, error) {
	if _, err := os.Stat(s.activityPath()); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	dir := filepath.Join(s.vault, "archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := "activity-" + time.Now().UTC().Format("20060102T150405Z") + ".jsonl"
	dst := filepath.Join(dir, name)
	if err := os.Rename(s.activityPath(), dst); err != nil {
		return "", err
	}
	return dst, nil
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
// Empty sections render "_none_"; the title and resume line come first.
func buildHandoff(args createHandoffArgs, acts []activityEntry, git string) string {
	var b strings.Builder
	b.WriteString("# Vault Handoff\n")
	b.WriteString("Resume: continue from Next Immediate Action. Do NOT repeat anything in Failed Attempts.\n")
	b.WriteString("\n## Goal\n")
	b.WriteString(orNone(args.Goal))
	b.WriteString("\n\n## Architecture & Decisions\n")
	b.WriteString(orNone(args.Decisions))
	b.WriteString("\n\n## Touched Files & AST Scope\n")
	if files := dedupFiles(acts); len(files) == 0 {
		b.WriteString("_none_\n")
	} else {
		for _, f := range files {
			b.WriteString(f)
			b.WriteByte('\n')
		}
	}
	b.WriteString("AST scope: not computed (paths only)\n")
	b.WriteString("\n## Completed Work\n")
	b.WriteString(orNone(args.CompletedWork))
	b.WriteString("\n\n## Current Blocker & Active Errors\n")
	b.WriteString(orNone(args.Blocker))
	b.WriteByte('\n')
	b.WriteString(activeErrors(acts))
	b.WriteString("\n\n## Failed Attempts (Do Not Repeat)\n")
	if len(args.FailedAttempts) == 0 {
		b.WriteString("_none_\n")
	} else {
		for _, fa := range args.FailedAttempts {
			b.WriteString("- ")
			b.WriteString(fa)
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n## Next Immediate Action\n")
	b.WriteString(orNone(args.NextAction))
	b.WriteString("\n\n## Git State & Checkpoint Tag\n")
	b.WriteString(git)
	b.WriteByte('\n')
	// Feature D: the whole handoff is redacted, so secrets in any section
	// (agent-supplied summaries, stderr excerpts, git status) never persist.
	return heuristics.Redact(b.String())
}

// orNone renders an empty value as "_none_".
func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "_none_"
	}
	return s
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

// gitState returns the workspace path, branch, short HEAD hash and porcelain
// status of the resolved git dir, or "git unavailable" (never an error).
func (s *State) gitState(workspace string) string {
	dir := s.gitDir(workspace)
	branch, err1 := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	short, err2 := gitOutput(dir, "rev-parse", "--short", "HEAD")
	status, err3 := gitOutput(dir, "status", "--porcelain")
	if err1 != nil || err2 != nil || err3 != nil {
		return "git unavailable"
	}
	return fmt.Sprintf("workspace: %s\nbranch: %s\ncommit: %s\nstatus:\n%s", dir, branch, short, status)
}

// gitDir returns where git commands run: the workspace when it is an absolute
// path inside a git repo, else VAULT_ROOT. It never errors; fallbacks are
// logged to stderr.
func (s *State) gitDir(workspace string) string {
	if workspace != "" {
		if !filepath.IsAbs(workspace) {
			s.log.Printf("workspace %q is not absolute; falling back to VAULT_ROOT %s", workspace, s.root)
		} else if !isGitRepo(workspace) {
			s.log.Printf("workspace %q is not inside a git repo; falling back to VAULT_ROOT %s", workspace, s.root)
		} else {
			return workspace
		}
	}
	return s.root
}

// isGitRepo reports whether dir is inside a git repository.
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
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
