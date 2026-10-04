package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readSettings loads a settings file for assertions.
func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal settings: %v (%s)", err, data)
	}
	return cfg
}

// TestRegisterMCPSetsVaultEntry: a fresh (missing) settings file gets exactly
// one vault entry with the right transport, env and autoApprove list.
func TestRegisterMCPSetsVaultEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings", "cline_mcp_settings.json")
	backup, err := registerMCP(path, "/opt/vault/bin/vault", "/repo/root")
	if err != nil {
		t.Fatalf("registerMCP: %v", err)
	}
	if backup != "" {
		t.Errorf("backup = %q, want empty (file did not exist)", backup)
	}
	cfg := readSettings(t, path)
	servers := cfg["mcpServers"].(map[string]any)
	if len(servers) != 1 {
		t.Fatalf("mcpServers has %d entries, want 1: %v", len(servers), servers)
	}
	v := servers["vault"].(map[string]any)
	tr := v["transport"].(map[string]any)
	if tr["type"] != "stdio" || tr["command"] != "/opt/vault/bin/vault" || tr["cwd"] != "/repo/root" {
		t.Errorf("transport = %v", tr)
	}
	env := tr["env"].(map[string]any)
	if env["VAULT_ROOT"] != "/repo/root" {
		t.Errorf("env = %v", env)
	}
	if v["disabled"] != false {
		t.Errorf("disabled = %v, want false", v["disabled"])
	}
	apps := v["autoApprove"].([]any)
	if len(apps) != 4 {
		t.Fatalf("autoApprove = %v, want 4 tools", apps)
	}
	for i, want := range []string{"report_activity", "check_context_health", "create_handoff", "read_handoff"} {
		if apps[i] != want {
			t.Errorf("autoApprove[%d] = %v, want %s", i, apps[i], want)
		}
	}
}

// TestRegisterMCPIdempotentPreservesOthers: registering twice over an
// existing file with another server keeps the other server byte-for-byte
// intact, replaces only vault, and backs the file up on the second run.
func TestRegisterMCPIdempotentPreservesOthers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cline_mcp_settings.json")
	other := `{"mcpServers":{"other":{"command":"/usr/bin/other-server","disabled":true,"env":{"K":"V"}}}}`
	if err := os.WriteFile(path, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		backup, err := registerMCP(path, "/opt/vault/bin/vault", "/repo/root")
		if err != nil {
			t.Fatalf("registerMCP run %d: %v", i+1, err)
		}
		// The settings file exists from the very first run, so every run
		// backs it up.
		if backup == "" || !strings.HasPrefix(backup, path+".bak-") {
			t.Errorf("run %d backup = %q, want <path>.bak-<ts>", i+1, backup)
		}
	}

	cfg := readSettings(t, path)
	servers := cfg["mcpServers"].(map[string]any)
	if len(servers) != 2 {
		t.Fatalf("mcpServers has %d entries, want 2 (vault + other): %v", len(servers), servers)
	}
	o := servers["other"].(map[string]any)
	if o["command"] != "/usr/bin/other-server" || o["disabled"] != true {
		t.Errorf("other server clobbered: %v", o)
	}
	if o["env"].(map[string]any)["K"] != "V" {
		t.Errorf("other server env clobbered: %v", o)
	}
	v := servers["vault"].(map[string]any)
	if v["transport"].(map[string]any)["command"] != "/opt/vault/bin/vault" {
		t.Errorf("vault entry wrong: %v", v)
	}

	// Each run backed up the pre-merge content; the latest backup must
	// contain the original other-server config.
	baks, _ := filepath.Glob(path + ".bak-*")
	if len(baks) != 2 {
		t.Fatalf("backups = %v, want exactly 2", baks)
	}
	bakData, err := os.ReadFile(baks[len(baks)-1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bakData), "/usr/bin/other-server") {
		t.Errorf("backup does not contain original content: %s", bakData)
	}
}

// TestUnregisterMCPRemovesOnlyVault: unregister deletes the vault entry and
// leaves every other server untouched, backing the file up first.
func TestUnregisterMCPRemovesOnlyVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cline_mcp_settings.json")
	src := `{"mcpServers":{"vault":{"command":"/x"},"other":{"command":"/y"}}}`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	backup, err := unregisterMCP(path)
	if err != nil {
		t.Fatalf("unregisterMCP: %v", err)
	}
	if backup == "" {
		t.Error("backup = empty, want a .bak- path for an existing file")
	}
	cfg := readSettings(t, path)
	servers := cfg["mcpServers"].(map[string]any)
	if _, ok := servers["vault"]; ok {
		t.Errorf("vault entry still present: %v", servers)
	}
	if servers["other"].(map[string]any)["command"] != "/y" {
		t.Errorf("other server lost: %v", servers)
	}
}

// TestUnregisterMCPMissingFileIsNoOp: uninstalling with no settings file must
// succeed without creating anything.
func TestUnregisterMCPMissingFileIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "cline_mcp_settings.json")
	backup, err := unregisterMCP(path)
	if err != nil {
		t.Fatalf("unregisterMCP: %v", err)
	}
	if backup != "" {
		t.Errorf("backup = %q, want empty", backup)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("settings file was created by unregister: %v", err)
	}
}
