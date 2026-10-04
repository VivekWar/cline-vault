// Command vault is the Vault MCP server over stdio.
//
// stdout carries ONLY JSON-RPC 2.0 responses, one per line. All logs go to
// stderr. The project root is VAULT_ROOT when set, else the working
// directory; root/.vault is created on demand by the state package at the
// first write.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"vault/internal/mcp"
	"vault/internal/state"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	if len(os.Args) > 1 && os.Args[1] == "health" {
		runHealth(os.Args[2:])
		return
	}

	srv := mcp.NewServer(os.Stdin, os.Stdout, resolveRoot())
	if err := srv.Serve(); err != nil {
		log.Printf("vault: fatal: %v", err)
		os.Exit(1)
	}
}

// runHealth implements `vault health [--root DIR] [--workspace DIR]`: it
// prints the Health JSON to stdout and exits 0.
func runHealth(args []string) {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "", "vault root (default VAULT_ROOT or cwd)")
	workspace := fs.String("workspace", "", "workspace directory to snapshot (default root)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	r := *root
	if r == "" {
		r = resolveRoot()
	}
	abs, err := filepath.Abs(r)
	if err != nil {
		abs = r
	}

	st := state.New(abs)
	out, err := st.HealthJSON(*workspace)
	if err != nil {
		log.Printf("vault health: %v", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, out)
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
