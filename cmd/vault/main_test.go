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
