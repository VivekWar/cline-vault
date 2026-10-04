// Package main — MCP settings registration for `make install`/`make uninstall`.
//
// These helpers merge the "vault" MCP server entry into Cline's
// cline_mcp_settings.json idempotently: only the vault entry is touched,
// every other server survives, and an existing settings file is backed up
// before it is rewritten.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// vaultTools are the four MCP tools auto-approved at install time.
var vaultTools = []string{"report_activity", "check_context_health", "create_handoff", "read_handoff"}

// registerMCP merges a "vault" entry into the Cline MCP settings file at
// settingsPath and returns the backup path created ("" when the file did not
// exist yet). binPath is the absolute path to the vault binary; root is the
// repo root used as VAULT_ROOT. Any existing "vault" entry is replaced;
// every other server is left untouched.
func registerMCP(settingsPath, binPath, root string) (backup string, err error) {
	cfg, hadFile, err := readMCPConfig(settingsPath)
	if err != nil {
		return "", err
	}
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		servers = map[string]any{}
		cfg["mcpServers"] = servers
	}
	servers["vault"] = map[string]any{
		"transport": map[string]any{
			"type":    "stdio",
			"command": binPath,
			"cwd":     root,
			"env":     map[string]string{"VAULT_ROOT": root},
		},
		"autoApprove": vaultTools,
		"disabled":    false,
	}
	if hadFile {
		backup, err = backupSettings(settingsPath)
		if err != nil {
			return "", err
		}
	}
	if err := writeMCPConfig(settingsPath, cfg); err != nil {
		return backup, err
	}
	return backup, nil
}

// unregisterMCP removes only the "vault" entry from the settings file. A
// missing file is a no-op; a present file is backed up before rewriting.
func unregisterMCP(settingsPath string) (backup string, err error) {
	cfg, hadFile, err := readMCPConfig(settingsPath)
	if err != nil {
		return "", err
	}
	if !hadFile {
		return "", nil // nothing to remove, nothing to create
	}
	servers, ok := cfg["mcpServers"].(map[string]any)
	if ok {
		delete(servers, "vault")
	}
	backup, err = backupSettings(settingsPath)
	if err != nil {
		return "", err
	}
	if err := writeMCPConfig(settingsPath, cfg); err != nil {
		return backup, err
	}
	return backup, nil
}

// readMCPConfig loads the settings file as a generic JSON object. A missing
// file yields an empty config and hadFile=false.
func readMCPConfig(path string) (cfg map[string]any, hadFile bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	cfg = map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, false, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	return cfg, true, nil
}

// backupSettings copies the current settings file to
// <path>.bak-<UTC timestamp> (with a numeric suffix when a backup already
// exists in the same second) and returns the backup path.
func backupSettings(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	base := path + ".bak-" + time.Now().UTC().Format("20060102T150405Z")
	bak := base
	for i := 1; ; i++ {
		if _, err := os.Stat(bak); os.IsNotExist(err) {
			break
		}
		bak = fmt.Sprintf("%s-%d", base, i)
	}
	if err := os.WriteFile(bak, data, 0o644); err != nil {
		return "", err
	}
	return bak, nil
}

// writeMCPConfig serializes cfg as indented JSON (0644) at path, creating
// parent directories as needed.
func writeMCPConfig(path string, cfg map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// runMCPRegister implements the hidden `vault mcp-register` subcommand used
// by scripts/install.sh:
//
//	vault mcp-register --settings PATH --root ROOT      (add/replace vault entry)
//	vault mcp-register --unregister --settings PATH     (remove vault entry only)
func runMCPRegister(args []string) {
	fs := flag.NewFlagSet("mcp-register", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	settings := fs.String("settings", "", "path to cline_mcp_settings.json")
	root := fs.String("root", "", "vault repo root (VAULT_ROOT for the MCP server)")
	unregister := fs.Bool("unregister", false, "remove the vault entry instead of adding it")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *settings == "" {
		log.Printf("vault mcp-register: --settings is required")
		os.Exit(2)
	}
	if *unregister {
		backup, err := unregisterMCP(*settings)
		if err != nil {
			log.Printf("vault mcp-register: %v", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "removed vault MCP entry from %s\n", *settings)
		if backup != "" {
			fmt.Fprintf(os.Stdout, "backup: %s\n", backup)
		}
		return
	}
	if *root == "" {
		log.Printf("vault mcp-register: --root is required")
		os.Exit(2)
	}
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	bin, err := filepath.Abs(exe)
	if err != nil {
		bin = exe
	}
	backup, err := registerMCP(*settings, bin, absRoot(*root))
	if err != nil {
		log.Printf("vault mcp-register: %v", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "registered vault MCP server in %s\n", *settings)
	if backup != "" {
		fmt.Fprintf(os.Stdout, "backup: %s\n", backup)
	}
}
