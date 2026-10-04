// Package heuristics implements Vault's deterministic, offline context-rot
// detection: H1 recurring error loop (Jaccard similarity over normalized
// output) and H2 code oscillation (net-to-gross churn over exact git tree
// snapshots), plus the Health aggregator that turns those into a score.
package heuristics

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// Entry is a minimal activity record for detection. Output is the combined
// stdout+stderr of the command (the "stderr" field on the wire).
type Entry struct {
	Kind     string
	ExitCode int
	Output   string
}

// Flag is one detected context-rot signal.
type Flag struct {
	Name     string `json:"name"`
	Evidence any    `json:"evidence"`
}

// LoopEvidence is the evidence attached to RECURRING_ERROR_LOOP.
type LoopEvidence struct {
	Similarities []float64 `json:"similarities"`
	SharedTokens []string  `json:"shared_tokens"`
}

var (
	// pathExtRe matches a source-file extension word (with optional :line:col)
	// so "calc_test.go:41:" is recognized as a path even without a slash.
	pathExtRe    = regexp.MustCompile(`\.(?:go|py|ts|js)\b`)
	hexRe        = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	rfc3339Re    = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?`)
	clockRe      = regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}(?:\.\d+)?\b`)
	durRe        = regexp.MustCompile(`\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h)\b`)
	numRe        = regexp.MustCompile(`\b\d+\b`)
	exitStatusRe = regexp.MustCompile(`^exit status \d+$`)
	nonAlnumRe   = regexp.MustCompile(`[^a-zA-Z0-9]+`)
)

// Normalize lowercases, strips volatile tokens (paths, hex addresses,
// timestamps, durations, bare numbers) and Go test boilerplate, then splits on
// non-alphanumeric characters, dropping tokens shorter than 2 characters and
// de-duplicating. The result is a sorted token set.
func Normalize(output string) []string {
	set := map[string]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isBoilerplateLine(trimmed) {
			continue
		}
		for _, field := range strings.Fields(trimmed) {
			if isPathToken(field) {
				continue
			}
			for _, tok := range nonAlnumRe.Split(stripVolatile(field), -1) {
				tok = strings.ToLower(tok)
				if len(tok) < 2 {
					continue
				}
				set[tok] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// isBoilerplateLine reports whether a trimmed line is Go test boilerplate.
func isBoilerplateLine(s string) bool {
	switch {
	case s == "PASS", s == "FAIL", s == "=== RUN", s == "--- PASS":
		return true
	case strings.HasPrefix(s, "ok  "):
		return true
	case strings.HasPrefix(s, "FAIL\t"):
		return true
	case exitStatusRe.MatchString(s):
		return true
	}
	return false
}

// isPathToken reports whether a whitespace-delimited field is a file path: it
// contains a slash/backslash, or ends in a source extension (.go/.py/.ts/.js)
// with optional :line:col.
func isPathToken(s string) bool {
	if strings.ContainsAny(s, "/\\") {
		return true
	}
	return pathExtRe.MatchString(s)
}

// stripVolatile replaces hex addresses, timestamps, durations and bare numbers
// with spaces so only stable words remain.
func stripVolatile(s string) string {
	s = hexRe.ReplaceAllString(s, " ")
	s = rfc3339Re.ReplaceAllString(s, " ")
	s = clockRe.ReplaceAllString(s, " ")
	s = durRe.ReplaceAllString(s, " ")
	return numRe.ReplaceAllString(s, " ")
}

// Jaccard returns the Jaccard similarity of two token sets: |A∩B| / |A∪B|.
// Both inputs must already be de-duplicated sets (as produced by Normalize).
// Two empty sets yield 0.
func Jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inB := make(map[string]struct{}, len(b))
	for _, t := range b {
		inB[t] = struct{}{}
	}
	inter := 0
	for _, t := range a {
		if _, ok := inB[t]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// DetectErrorLoop flags RECURRING_ERROR_LOOP when the last 3 COMMAND/TEST
// entries all fail (exit_code != 0) with non-empty output and every pairwise
// Jaccard similarity of their normalized output is >= the threshold. Entry
// order (not timestamps) defines "last 3".
func DetectErrorLoop(entries []Entry) *Flag {
	var relevant []Entry
	for _, e := range entries {
		if e.Kind == "COMMAND" || e.Kind == "TEST" {
			relevant = append(relevant, e)
		}
	}
	if len(relevant) < 3 {
		return nil
	}
	last3 := relevant[len(relevant)-3:]
	sets := make([][]string, 0, 3)
	for _, e := range last3 {
		if e.ExitCode == 0 {
			return nil
		}
		if strings.TrimSpace(e.Output) == "" {
			return nil
		}
		sets = append(sets, Normalize(e.Output))
	}
	sims := []float64{
		Jaccard(sets[0], sets[1]),
		Jaccard(sets[1], sets[2]),
		Jaccard(sets[0], sets[2]),
	}
	for _, s := range sims {
		if s < jaccardMin() {
			return nil
		}
	}
	return &Flag{
		Name: "RECURRING_ERROR_LOOP",
		Evidence: LoopEvidence{
			Similarities: round2All(sims),
			SharedTokens: sharedTokens(sets),
		},
	}
}

// sharedTokens returns tokens present in every set, sorted.
func sharedTokens(sets [][]string) []string {
	count := map[string]int{}
	for _, s := range sets {
		for _, t := range s {
			count[t]++
		}
	}
	var out []string
	for t, c := range count {
		if c == len(sets) {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// round2 rounds v to 2 decimal places.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// round2All rounds each value to 2 decimals.
func round2All(vs []float64) []float64 {
	out := make([]float64, len(vs))
	for i, v := range vs {
		out[i] = round2(v)
	}
	return out
}
