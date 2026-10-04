package heuristics

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// OscillationEvidence is the evidence attached to CODE_OSCILLATION_THRASHING.
type OscillationEvidence struct {
	Net        float64 `json:"net"`
	Gross      float64 `json:"gross"`
	Efficiency float64 `json:"efficiency"`
}

// snapshotFn and diffNumstatFn are the git-executing seams; tests replace them
// to avoid touching a real repository.
var (
	snapshotFn    = gitSnapshot
	diffNumstatFn = gitDiffNumstat
)

// Snapshot returns the tree hash of the working tree WITHOUT touching the real
// index: it copies the current index file to a temp file, sets GIT_INDEX_FILE
// to that temp file, runs `git add -A` then `git write-tree`, then removes the
// temp file. This works inside git worktrees (git rev-parse --git-path index
// resolves the per-worktree index). Any failure (e.g. not a git repo) returns
// "" and never errors.
func Snapshot(workspace string) string {
	return snapshotFn(workspace)
}

func gitSnapshot(workspace string) string {
	indexPath, err := gitOutput(workspace, "rev-parse", "--git-path", "index")
	if err != nil {
		return ""
	}
	// --git-path can return a path relative to the working tree (e.g.
	// ".git/index"); resolve it against workspace so the copy below is
	// unambiguous regardless of the process cwd.
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(workspace, indexPath)
	}
	tmp, err := os.CreateTemp("", "vault-index-*")
	if err != nil {
		return ""
	}
	tmpName := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpName)

	// Copy the existing index if present (a fresh repo may not have one yet).
	if data, err := os.ReadFile(indexPath); err == nil {
		if err := os.WriteFile(tmpName, data, 0o644); err != nil {
			return ""
		}
	}

	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpName)
	if _, err := gitEnv(workspace, env, "add", "-A"); err != nil {
		return ""
	}
	tree, err := gitEnv(workspace, env, "write-tree")
	if err != nil {
		return ""
	}
	return tree
}

// Churn computes net, gross and the distinct-snapshot count over consecutive
// tree snapshots. gross is the sum over consecutive pairs of (added+deleted);
// net is (added+deleted) from the first to the last tree; snapshots is the
// count after skipping empty trees and consecutive duplicates. Binary files
// count as 0.
func Churn(workspace string, trees []string) (net, gross float64, snapshots int) {
	ts := compactTrees(trees)
	snapshots = len(ts)
	if len(ts) < 2 {
		return 0, 0, snapshots
	}
	for i := 0; i < len(ts)-1; i++ {
		out, err := diffNumstatFn(workspace, ts[i], ts[i+1])
		if err != nil {
			continue
		}
		a, d := parseNumstat(out)
		gross += float64(a + d)
	}
	out, err := diffNumstatFn(workspace, ts[0], ts[len(ts)-1])
	if err != nil {
		return 0, gross, snapshots
	}
	a, d := parseNumstat(out)
	net = float64(a + d)
	return net, gross, snapshots
}

// compactTrees drops empty trees and consecutive duplicates.
func compactTrees(trees []string) []string {
	var out []string
	for _, t := range trees {
		if t == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == t {
			continue
		}
		out = append(out, t)
	}
	return out
}

// parseNumstat parses `git diff --numstat` output into total added and deleted
// lines; binary files ("-") and unparseable columns count as 0.
func parseNumstat(out string) (added, deleted int) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		added += numOrZero(fields[0])
		deleted += numOrZero(fields[1])
	}
	return added, deleted
}

// numOrZero parses a numstat column, treating "-" (binary) and junk as 0.
func numOrZero(s string) int {
	if s == "-" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// churnEfficiency is the net-to-gross ratio (0 when gross is 0).
func churnEfficiency(net, gross float64) float64 {
	if gross == 0 {
		return 0
	}
	return net / gross
}

// DetectOscillation flags CODE_OSCILLATION_THRASHING when churn is
// inefficient: net/gross is below the efficiency threshold AND either the line
// churn exceeds the gross threshold or at least churnMinSnapshots distinct
// snapshots were taken. The snapshot gate catches micro-oscillations (e.g.
// flipping a single line back and forth) whose line churn stays small.
func DetectOscillation(net, gross float64, snapshots int) *Flag {
	eff := churnEfficiency(net, gross)
	// Flag if we exceed the line threshold OR if we've thrashed for 10+ snapshots
	if (gross > churnMinGross() || snapshots >= churnMinSnapshots()) && eff < churnMaxEff() {
		return &Flag{
			Name: "CODE_OSCILLATION_THRASHING",
			Evidence: OscillationEvidence{
				Net:        round2(net),
				Gross:      round2(gross),
				Efficiency: round2(eff),
			},
		}
	}
	return nil
}

// gitOutput runs git in dir and returns trimmed stdout ("" on error).
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitEnv runs git in dir with extra environment entries and returns trimmed
// stdout ("" on error).
func gitEnv(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitDiffNumstat returns `git diff --numstat a b` stdout for trees a,b run in
// workspace.
func gitDiffNumstat(workspace, a, b string) (string, error) {
	cmd := exec.Command("git", "diff", "--numstat", a, b)
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
