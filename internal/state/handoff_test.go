package state

import (
	"encoding/json"
	"strings"
	"testing"
)

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
