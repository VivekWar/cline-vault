// Package state owns Vault's persisted state under <root>/.vault/: the
// activity log (activity.jsonl) and the handoff state file.
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"vault/internal/heuristics"
)

// State manages Vault's state directory rooted at root.
type State struct {
	root  string // absolute project root (VAULT_ROOT or cwd)
	vault string // root/.vault, created on demand at the first write
	log   *log.Logger
}

// New returns a State for the given root, resolved to an absolute path.
// It does NOT create root/.vault; ensureVault does that on first write.
func New(root string) *State {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return &State{root: abs, vault: filepath.Join(abs, ".vault"), log: log.New(os.Stderr, "", 0)}
}

// Root returns the absolute root path.
func (s *State) Root() string { return s.root }

// ensureVault creates root/.vault on demand.
func (s *State) ensureVault() error {
	return os.MkdirAll(s.vault, 0o755)
}

// activityPath returns the path to the activity log.
func (s *State) activityPath() string { return filepath.Join(s.vault, "activity.jsonl") }

// activityEntry is one JSON line in activity.jsonl.
type activityEntry struct {
	Time      string   `json:"time"`
	Kind      string   `json:"kind"`
	Command   string   `json:"command"`
	ExitCode  int      `json:"exit_code"`
	Stderr    string   `json:"stderr"`
	Files     []string `json:"files"`
	Workspace string   `json:"workspace"`
	Tree      string   `json:"tree,omitempty"`
}

// validKinds is the fixed enum of activity kinds.
var validKinds = map[string]bool{
	"READ": true, "EDIT": true, "COMMAND": true, "TEST": true, "COMMIT": true,
}

// DispatchTool runs the named tool and returns its text output and isError.
func (s *State) DispatchTool(name string, arguments json.RawMessage) (string, bool) {
	switch name {
	case "report_activity":
		return s.reportActivity(arguments)
	case "check_context_health":
		return s.checkContextHealth(arguments)
	case "create_handoff":
		return s.createHandoff(arguments)
	case "read_handoff":
		return s.readHandoff()
	default:
		return "unknown tool: " + name, true
	}
}

// reportActivity appends one activity line and returns "recorded #N".
func (s *State) reportActivity(argsJSON json.RawMessage) (string, bool) {
	var args struct {
		Kind      string   `json:"kind"`
		Command   string   `json:"command"`
		ExitCode  int      `json:"exit_code"`
		Stderr    string   `json:"stderr"`
		Files     []string `json:"files"`
		Workspace string   `json:"workspace"`
	}
	if len(argsJSON) > 0 && string(argsJSON) != "null" {
		if err := json.Unmarshal(argsJSON, &args); err != nil {
			return fmt.Sprintf("invalid arguments: %v", err), true
		}
	}
	if !validKinds[args.Kind] {
		return fmt.Sprintf("invalid kind %q (want one of READ, EDIT, COMMAND, TEST, COMMIT)", args.Kind), true
	}
	if err := s.ensureVault(); err != nil {
		return fmt.Sprintf("cannot create state dir: %v", err), true
	}
	if args.Files == nil {
		args.Files = []string{}
	}
	// Snapshot the git tree for churn tracking. READ activities are pure
	// observations: skipping the snapshot avoids a full `git add -A` +
	// `git write-tree` per read (compactTrees ignores empty trees anyway).
	tree := ""
	if args.Kind != "READ" {
		tree = heuristics.Snapshot(s.gitDir(args.Workspace))
	}
	entry := activityEntry{
		Time:      time.Now().UTC().Format(time.RFC3339),
		Kind:      args.Kind,
		Command:   args.Command,
		ExitCode:  args.ExitCode,
		Stderr:    args.Stderr,
		Files:     args.Files,
		Workspace: args.Workspace,
		Tree:      tree,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Sprintf("marshal activity: %v", err), true
	}
	line = append(line, '\n')
	f, err := os.OpenFile(s.activityPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Sprintf("open activity log: %v", err), true
	}
	// Single Write call including the trailing newline.
	if _, err := f.Write(line); err != nil {
		f.Close()
		return fmt.Sprintf("write activity log: %v", err), true
	}
	if err := f.Close(); err != nil {
		return fmt.Sprintf("close activity log: %v", err), true
	}
	n, err := s.countActivities()
	if err != nil {
		return fmt.Sprintf("count activities: %v", err), true
	}
	return fmt.Sprintf("recorded #%d", n), false
}

// Directive prefixes for the check_context_health verdict line.
const (
	verdictHealthy  = "VERDICT: HEALTHY. Continue working."
	verdictDegraded = "VERDICT: DEGRADED. Stop what you are doing. You are thrashing. " +
		"Call create_handoff immediately and ask the user to start a new task."
)

// checkContextHealth returns a natural-language directive followed by the
// aggregated Health JSON for the session since the last rotation (a fresh
// snapshot of workspace, or VAULT_ROOT, acts as the latest point). Healthy
// sessions get the continue directive; degraded and critical sessions both get
// the stop-and-handoff directive.
func (s *State) checkContextHealth(argsJSON json.RawMessage) (string, bool) {
	var args struct {
		Workspace string `json:"workspace"`
	}
	if len(argsJSON) > 0 && string(argsJSON) != "null" {
		if err := json.Unmarshal(argsJSON, &args); err != nil {
			return fmt.Sprintf("invalid arguments: %v", err), true
		}
	}
	data, err := s.HealthJSON(args.Workspace)
	if err != nil {
		return fmt.Sprintf("cannot assess health: %v", err), true
	}
	var h heuristics.Health
	if err := json.Unmarshal([]byte(data), &h); err != nil {
		return fmt.Sprintf("cannot decode health: %v", err), true
	}
	verdict := verdictHealthy
	if h.Status != "healthy" {
		verdict = verdictDegraded
	}
	return verdict + "\n\n" + data, false
}

// HealthJSON computes the current context health for workspace (or root) and
// returns it as a JSON object string.
func (s *State) HealthJSON(workspace string) (string, error) {
	entries, err := s.readActivities()
	if err != nil {
		return "", err
	}
	hsEntries := make([]heuristics.Entry, 0, len(entries))
	trees := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		hsEntries = append(hsEntries, heuristics.Entry{
			Kind:     e.Kind,
			ExitCode: e.ExitCode,
			Output:   e.Stderr,
		})
		if e.Tree != "" {
			trees = append(trees, e.Tree)
		}
	}
	ws := s.gitDir(workspace)
	if tree := heuristics.Snapshot(ws); tree != "" {
		trees = append(trees, tree)
	}
	health := heuristics.Assess(ws, hsEntries, trees)
	data, err := json.Marshal(health)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// countActivities returns the number of lines in activity.jsonl (0 if absent).
func (s *State) countActivities() (int, error) {
	data, err := os.ReadFile(s.activityPath())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return bytes.Count(data, []byte{'\n'}), nil
}

// readHandoff returns the full text of handoff_state.md, or an error result
// when no handoff has been written yet.
func (s *State) readHandoff() (string, bool) {
	data, err := os.ReadFile(filepath.Join(s.vault, "handoff_state.md"))
	if errors.Is(err, os.ErrNotExist) {
		return "no handoff yet", true
	}
	if err != nil {
		return fmt.Sprintf("cannot read handoff: %v", err), true
	}
	return string(data), false
}
