package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
// log built from the loop fixtures and expects RECURRING_ERROR_LOOP with
// status "degraded".
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
	var h struct {
		Score  int    `json:"score"`
		Status string `json:"status"`
		Flags  []struct {
			Name string `json:"name"`
		} `json:"flags"`
	}
	if err := json.Unmarshal([]byte(text), &h); err != nil {
		t.Fatalf("unmarshal health: %v (%s)", err, text)
	}
	if h.Status != "degraded" {
		t.Errorf("status = %q, want degraded (%s)", h.Status, text)
	}
	found := false
	for _, f := range h.Flags {
		if f.Name == "RECURRING_ERROR_LOOP" {
			found = true
		}
	}
	if !found {
		t.Errorf("flags missing RECURRING_ERROR_LOOP: %s", text)
	}
}
