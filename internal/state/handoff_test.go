package state

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// footerRe extracts X, Y and Z from the compression footer line.
var footerRe = regexp.MustCompile(`Condensed ~(\d+) tokens of activity history into ~(\d+) tokens of handoff state\. \(Saved ~(\d+) tokens\)\.`)

// TestHandoffCompressionFooter: the footer sits at the very bottom of
// handoff_state.md with tokens computed as characters/4. X is the total
// characters of all Command and Stderr fields in the activity log; Y matches
// the final handoff length; saved is clamped at 0.
func TestHandoffCompressionFooter(t *testing.T) {
	cases := []struct {
		name     string
		entries  []string // report_activity payloads
		xTokens  int
		wantSave bool // positive savings expected
	}{
		{
			name: "small history clamps saved to zero",
			entries: []string{
				`{"kind":"COMMAND","command":"abcd","exit_code":0,"stderr":"efgh","files":[]}`,
			},
			xTokens:  2, // (4+4)/4
			wantSave: false,
		},
		{
			name: "large history saves tokens",
			entries: []string{
				`{"kind":"COMMAND","command":"` + strings.Repeat("x", 3996) + `","exit_code":0,"stderr":"` + strings.Repeat("y", 3996) + `","files":[]}`,
			},
			xTokens:  (3996 + 3996) / 4,
			wantSave: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := New(t.TempDir())
			for _, e := range tc.entries {
				mustReport(t, st, e)
			}
			if _, isErr := st.createHandoff(json.RawMessage(`{"goal":"G","next_action":"NA"}`)); isErr {
				t.Fatal("createHandoff failed")
			}
			content := readHandoffFile(t, st)
			if !strings.HasSuffix(content, ".\n") || !strings.Contains(content, "---\nVault Compression Estimate: ") {
				t.Fatalf("footer missing or not at the bottom:\n%s", content)
			}
			lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
			last := lines[len(lines)-1]
			m := footerRe.FindStringSubmatch(last)
			if m == nil {
				t.Fatalf("footer line does not match format: %q", last)
			}
			x, y, z := atoi(t, m[1]), atoi(t, m[2]), atoi(t, m[3])
			if x != tc.xTokens {
				t.Errorf("X = %d tokens, want %d (footer: %s)", x, tc.xTokens, last)
			}
			if wantY := len(content) / 4; y != wantY {
				t.Errorf("Y = %d tokens, want %d = len(handoff)/4 (footer: %s)", y, wantY, last)
			}
			if tc.wantSave {
				if z != x-y {
					t.Errorf("Z = %d, want %d = X - Y (footer: %s)", z, x-y, last)
				}
			} else if z != 0 {
				t.Errorf("Z = %d, want 0 (savings clamped; footer: %s)", z, last)
			}
		})
	}
}

// TestCompressionFooterCountsOnlyCommandAndStderr: the X estimate ignores
// files, workspace and other fields; it sums only Command and Stderr.
func TestCompressionFooterCountsOnlyCommandAndStderr(t *testing.T) {
	acts := []activityEntry{
		{Command: "abcd", Stderr: "efgh", Files: []string{"f.go", "g.go"}},
		{Command: "ij", Stderr: "klmnop"},
	}
	footer := compressionFooter(acts, "body")
	m := footerRe.FindStringSubmatch(footer)
	if m == nil {
		t.Fatalf("footer does not match format: %q", footer)
	}
	if x := atoi(t, m[1]); x != (8+8)/4 {
		t.Errorf("X = %d tokens, want 4 ((4+4+2+6)/4): %s", x, footer)
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("atoi(%q): %v", s, err)
	}
	return n
}

// TestHandoffRedactsSecrets: secrets carried in agent-supplied sections and in
// activity stderr must never persist in handoff_state.md.
func TestHandoffRedactsSecrets(t *testing.T) {
	st := New(t.TempDir())
	mustReport(t, st, `{"kind":"TEST","command":"go test","exit_code":1,`+
		`"stderr":"-----BEGIN PRIVATE KEY-----\nAAA\n-----END PRIVATE KEY-----\nfail","files":[]}`)
	args := `{"goal":"G","decisions":"use API_KEY=sk-abcdefghijklmnopqrstuvwxyz",` +
		`"completed_work":"CW","blocker":"B","failed_attempts":[],` +
		`"next_action":"export PASSWORD=hunter2 then run"}`
	if _, isErr := st.createHandoff(json.RawMessage(args)); isErr {
		t.Fatal("createHandoff failed")
	}
	content := readHandoffFile(t, st)
	for _, leak := range []string{
		"sk-abcdefghijklmnopqrstuvwxyz",
		"-----BEGIN PRIVATE KEY-----",
		"hunter2",
	} {
		if strings.Contains(content, leak) {
			t.Errorf("secret leaked into handoff_state.md: %q", leak)
		}
	}
	if got := strings.Count(content, "[REDACTED]"); got < 3 {
		t.Errorf("handoff has %d [REDACTED] markers, want >= 3:\n%s", got, content)
	}
}

// TestHandoffKeepsValidJSONArgs: the redaction pass must not disturb normal
// handoff rendering (still starts with the title, sections intact).
func TestHandoffKeepsValidJSONArgs(t *testing.T) {
	st := New(t.TempDir())
	if _, isErr := st.createHandoff(json.RawMessage(`{"goal":"G","next_action":"NA"}`)); isErr {
		t.Fatal("createHandoff failed")
	}
	content := readHandoffFile(t, st)
	if !strings.HasPrefix(content, "# Vault Handoff\n") {
		t.Errorf("handoff title lost: %q", content)
	}
	if !strings.Contains(content, "## Next Immediate Action\nNA") {
		t.Errorf("next action section lost: %q", content)
	}
}
