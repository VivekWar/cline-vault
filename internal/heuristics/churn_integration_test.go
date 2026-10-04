package heuristics

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "base.txt")
	git("commit", "-q", "-m", "base")
	return dir
}

func TestChurnIntegration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	t.Run("oscillation flags", func(t *testing.T) {
		ws := initGitRepo(t)
		f := filepath.Join(ws, "f.txt")
		writeLines := func(n int) {
			var b strings.Builder
			for i := 0; i < n; i++ {
				fmt.Fprintf(&b, "line %d\n", i)
			}
			if err := os.WriteFile(f, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		var trees []string
		writeLines(60)
		trees = append(trees, Snapshot(ws))
		for i := 0; i < 3; i++ {
			if err := os.WriteFile(f, []byte(""), 0o644); err != nil { // revert to empty
				t.Fatal(err)
			}
			trees = append(trees, Snapshot(ws))
			writeLines(60)
			trees = append(trees, Snapshot(ws))
		}
		for _, tr := range trees {
			if tr == "" {
				t.Fatalf("empty tree in %v", trees)
			}
		}
		net, gross, snapshots := Churn(ws, trees)
		if f := DetectOscillation(net, gross, snapshots); f == nil {
			t.Fatalf("want oscillation flag, net=%v gross=%v (trees=%d)", net, gross, len(trees))
		}
	})

	t.Run("micro oscillation flags", func(t *testing.T) {
		ws := initGitRepo(t)
		f := filepath.Join(ws, "micro.txt")
		if err := os.WriteFile(f, []byte("if x > y {\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var trees []string
		trees = append(trees, Snapshot(ws)) // ">"
		for i := 0; i < 12; i++ {
			content := "if x > y {\n"
			if i%2 == 0 {
				content = "if x >= y {\n"
			}
			if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			trees = append(trees, Snapshot(ws))
		}
		// 13 snapshots, gross = 12 flips * 2 lines = 24, net = 0 (ends on ">").
		net, gross, snapshots := Churn(ws, trees)
		if f := DetectOscillation(net, gross, snapshots); f == nil {
			t.Fatalf("want micro oscillation flag, net=%v gross=%v snapshots=%d", net, gross, snapshots)
		}
		if gross >= 100 {
			t.Errorf("micro oscillation gross=%v, want well below 100 (line threshold must not be needed)", gross)
		}
	})

	t.Run("progress no flag", func(t *testing.T) {
		ws := initGitRepo(t)
		f := filepath.Join(ws, "g.txt")
		appendLines := func(n int) {
			fh, err := os.OpenFile(f, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < n; i++ {
				fmt.Fprintf(fh, "added line %d\n", i)
			}
			fh.Close()
		}
		var trees []string
		trees = append(trees, Snapshot(ws)) // base
		for i := 0; i < 3; i++ {
			appendLines(50)
			trees = append(trees, Snapshot(ws))
		}
		net, gross, snapshots := Churn(ws, trees)
		if f := DetectOscillation(net, gross, snapshots); f != nil {
			t.Fatalf("unexpected oscillation flag, net=%v gross=%v", net, gross)
		}
		if net != 150 || gross != 150 {
			t.Errorf("net=%v gross=%v, want 150 / 150", net, gross)
		}
	})

	t.Run("snapshot leaves index unchanged", func(t *testing.T) {
		ws := initGitRepo(t)
		if err := os.WriteFile(filepath.Join(ws, "h.txt"), []byte("hello\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cached := func() string {
			cmd := exec.Command("git", "diff", "--cached", "--name-only")
			cmd.Dir = ws
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("git diff --cached: %v", err)
			}
			return strings.TrimSpace(string(out))
		}
		if got := cached(); got != "" {
			t.Fatalf("index not clean before snapshot: %q", got)
		}
		for i := 0; i < 3; i++ {
			if Snapshot(ws) == "" {
				t.Fatal("snapshot returned empty tree")
			}
		}
		if got := cached(); got != "" {
			t.Fatalf("index polluted by snapshot: %q", got)
		}
	})
}

func TestSnapshotInWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := repoRoot(t)
	tree := Snapshot(root)
	if tree == "" {
		t.Skipf("module root %s is not a git repo", root)
	}
	if len(tree) != 40 {
		t.Errorf("tree hash length = %d, want 40 (%q)", len(tree), tree)
	}
}
