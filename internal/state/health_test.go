package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func healthFixture(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
	b, err := os.ReadFile(filepath.Join(dir, "testdata", "heuristics", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// TestCheckContextHealthLoopFlag drives check_context_health over an activity
// log built from the loop fixtures and expects the DEGRADED directive followed
// by Health JSON carrying RECURRING_ERROR_LOOP with status "degraded".
func TestCheckContextHealthLoopFlag(t *testing.T) {
	st := New(t.TempDir())
	for _, name := range []string{
		"loop_same_assertion_1.txt",
		"loop_same_assertion_2.txt",
		"loop_same_assertion_3.txt",
	} {
		out := healthFixture(t, name)
		args := fmt.Sprintf(`{"kind":"TEST","command":"go test","exit_code":1,"stderr":%q}`, out)
		mustReport(t, st, args)
	}
	text, isErr := st.checkContextHealth(json.RawMessage(`{}`))
	if isErr {
		t.Fatalf("checkContextHealth: %s", text)
	}
	wantVerdict := "VERDICT: DEGRADED. Stop what you are doing. You are thrashing. " +
		"Call create_handoff immediately and ask the user to start a new task.\n\n"
	if !strings.HasPrefix(text, wantVerdict) {
		t.Errorf("directive = %q, want prefix %q", text, wantVerdict)
	}
	jsonText := textAfterDirective(t, text)
	var h struct {
		Score  int    `json:"score"`
		Status string `json:"status"`
		Flags  []struct {
			Name string `json:"name"`
		} `json:"flags"`
	}
	if err := json.Unmarshal([]byte(jsonText), &h); err != nil {
		t.Fatalf("unmarshal health: %v (%s)", err, jsonText)
	}
	if h.Status != "degraded" {
		t.Errorf("status = %q, want degraded (%s)", h.Status, jsonText)
	}
	found := false
	for _, f := range h.Flags {
		if f.Name == "RECURRING_ERROR_LOOP" {
			found = true
		}
	}
	if !found {
		t.Errorf("flags missing RECURRING_ERROR_LOOP: %s", jsonText)
	}
}

// TestCheckContextHealthHealthyDirective: an empty activity log must yield the
// HEALTHY directive followed by valid Health JSON with status "healthy".
func TestCheckContextHealthHealthyDirective(t *testing.T) {
	st := New(t.TempDir())
	text, isErr := st.checkContextHealth(json.RawMessage(`{}`))
	if isErr {
		t.Fatalf("checkContextHealth: %s", text)
	}
	wantVerdict := "VERDICT: HEALTHY. Continue working.\n\n"
	if !strings.HasPrefix(text, wantVerdict) {
		t.Errorf("directive = %q, want prefix %q", text, wantVerdict)
	}
	jsonText := textAfterDirective(t, text)
	var h struct {
		Score  int    `json:"score"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(jsonText), &h); err != nil {
		t.Fatalf("unmarshal health: %v (%s)", err, jsonText)
	}
	if h.Status != "healthy" {
		t.Errorf("status = %q, want healthy (%s)", h.Status, jsonText)
	}
	if h.Score != 100 {
		t.Errorf("score = %d, want 100 (%s)", h.Score, jsonText)
	}
}

// textAfterDirective splits a check_context_health result into its JSON part.
func textAfterDirective(t *testing.T, text string) string {
	t.Helper()
	idx := strings.Index(text, "\n\n")
	if idx < 0 {
		t.Fatalf("no directive/JSON separator in %q", text)
	}
	return text[idx+2:]
}
