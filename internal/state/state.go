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
	entry := activityEntry{
		Time:      time.Now().UTC().Format(time.RFC3339),
		Kind:      args.Kind,
		Command:   args.Command,
		ExitCode:  args.ExitCode,
		Stderr:    args.Stderr,
		Files:     args.Files,
		Workspace: args.Workspace,
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

// checkContextHealth is a STUB: always score 100 until heuristics land. The
// optional workspace arg is accepted for API parity but unused by the stub.
func (s *State) checkContextHealth(argsJSON json.RawMessage) (string, bool) {
	var args struct {
		Workspace string `json:"workspace"`
	}
	if len(argsJSON) > 0 && string(argsJSON) != "null" {
		if err := json.Unmarshal(argsJSON, &args); err != nil {
			return fmt.Sprintf("invalid arguments: %v", err), true
		}
	}
	n, err := s.countActivities()
	if err != nil {
		return fmt.Sprintf("cannot count activities: %v", err), true
	}
	data, err := json.Marshal(map[string]any{
		"score":          100,
		"flags":          []any{},
		"reason":         "stub: heuristics not implemented yet",
		"activity_count": n,
	})
	if err != nil {
		return fmt.Sprintf("marshal health: %v", err), true
	}
	return string(data), false
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
