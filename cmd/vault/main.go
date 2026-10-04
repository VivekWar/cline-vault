// Command vault is the Vault MCP server over stdio.
//
// stdout carries ONLY JSON-RPC 2.0 responses, one per line. All logs go to
// stderr. The project root is VAULT_ROOT when set, else the working
// directory; root/.vault is created on demand by the state package at the
// first write.
package main

import (
	"log"
	"os"
	"path/filepath"

	"vault/internal/mcp"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	srv := mcp.NewServer(os.Stdin, os.Stdout, resolveRoot())
	if err := srv.Serve(); err != nil {
		log.Printf("vault: fatal: %v", err)
		os.Exit(1)
	}
}

// resolveRoot returns VAULT_ROOT (made absolute) or the working directory.
func resolveRoot() string {
	if env := os.Getenv("VAULT_ROOT"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return env
		}
		return abs
	}
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	return wd
}
