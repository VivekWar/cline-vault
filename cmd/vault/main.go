// Command vault is the Vault MCP server over stdio.
//
// stdout carries ONLY JSON-RPC 2.0 responses, one per line. All logs go to
// stderr. The project root is VAULT_ROOT when set, else the working
// directory; root/.vault is created on demand by the state package at the
// first write.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"vault/internal/mcp"
	"vault/internal/report"
	"vault/internal/state"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "health":
			runHealth(os.Args[2:])
			return
		case "report":
			runReport(os.Args[2:])
			return
		case "serve":
			runServe(os.Args[2:])
			return
		}
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

	st := state.New(absRoot(*root))
	out, err := st.HealthJSON(*workspace)
	if err != nil {
		log.Printf("vault health: %v", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, out)
}

// runReport implements the `vault report` subcommand with two forms:
//
//   - `vault report [--root DIR]` (no positional argument) generates the
//     self-contained HTML report at <root>/.vault/report.html (Feature C).
//   - `vault report [--root DIR] '<json-args>'` feeds the JSON arguments
//     payload to report_activity (which takes the git snapshot and appends
//     the activity line) and prints the tool result. The telemetry plugin
//     uses this form so every activity goes through the Go server instead of
//     direct file writes.
func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "", "vault root (default VAULT_ROOT or cwd)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() == 0 {
		runReportHTML(*root)
		return
	}

	payload := fs.Arg(0)
	st := state.New(absRoot(*root))
	text, isErr := st.DispatchTool("report_activity", json.RawMessage(payload))
	fmt.Fprintln(os.Stdout, text)
	if isErr {
		os.Exit(1)
	}
}

// runReportHTML implements `vault report` with no payload: it generates
// <root>/.vault/report.html and prints the path written.
func runReportHTML(root string) {
	path, err := report.Generate(absRoot(root))
	if err != nil {
		log.Printf("vault report: %v", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "wrote "+path)
}

// runServe implements `vault serve [--root DIR] [--addr :8080]`: it registers
// the dashboard routes on the DefaultServeMux — "/" serves the HTML shell
// (head + HTMX script + polling div) and "/content" serves only the inner
// content fragment that HTMX swaps in every second — then serves HTTP.
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "", "vault root (default VAULT_ROOT or cwd)")
	addr := fs.String("addr", ":8080", "listen address")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	r := absRoot(*root)
	http.HandleFunc("/", report.DashboardHandler(r))
	http.HandleFunc("/content", report.ContentHandler(r))
	log.Printf("vault serve: dashboard at http://localhost%s (root %s)", *addr, r)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Printf("vault serve: %v", err)
		os.Exit(1)
	}
}

// absRoot returns the absolute vault root for a CLI subcommand: the --root
// flag when given, else VAULT_ROOT, else the working directory.
func absRoot(flagRoot string) string {
	r := flagRoot
	if r == "" {
		r = resolveRoot()
	}
	abs, err := filepath.Abs(r)
	if err != nil {
		return r
	}
	return abs
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
