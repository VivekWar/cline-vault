package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
)

// runServer feeds input to a server writing into a buffer and returns stdout.
func runServer(t *testing.T, root, input string) string {
	t.Helper()
	var out bytes.Buffer
	srv := NewServer(strings.NewReader(input), &out, root)
	srv.log = log.New(io.Discard, "", 0) // keep stderr quiet in tests
	if err := srv.Serve(); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}
	return out.String()
}

// responseLines splits stdout into response lines (one per Write).
func responseLines(t *testing.T, out string) []string {
	t.Helper()
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// TestServeTranscript feeds a full session through the loop and asserts the
// protocol shape: (a) exactly 4 response lines — no response for the
// notification; (b) every line parses as JSON-RPC 2.0 with a matching id;
// (c) tools/list returns exactly the 3 tool names.
func TestServeTranscript(t *testing.T) {
	root := t.TempDir()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"report_activity","arguments":{"kind":"EDIT","command":"nano x.go","exit_code":0,"files":["a.go","b.go"]}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_handoff","arguments":{"goal":"demo","next_action":"commit"}}}`,
	}, "\n") + "\n"

	out := runServer(t, root, input)
	lines := responseLines(t, out)
	if len(lines) != 4 {
		t.Fatalf("(a) got %d response lines, want exactly 4 (notification must not be answered): %q", len(lines), out)
	}

	wantIDs := []string{"1", "2", "3", "4"}
	for i, line := range lines {
		var res struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			t.Fatalf("(b) line %d is not valid JSON: %v (%q)", i, err, line)
		}
		if res.JSONRPC != "2.0" {
			t.Errorf("(b) line %d jsonrpc = %q, want \"2.0\"", i, res.JSONRPC)
		}
		if string(res.ID) != wantIDs[i] {
			t.Errorf("(b) line %d id = %s, want %s", i, res.ID, wantIDs[i])
		}
		if len(res.Error) > 0 && string(res.Error) != "null" {
			t.Errorf("(b) line %d has unexpected error: %s", i, res.Error)
		}
	}

	// initialize echoes the client's protocolVersion.
	if !strings.Contains(lines[0], `"protocolVersion":"2025-03-26"`) {
		t.Errorf("initialize did not echo protocolVersion: %s", lines[0])
	}
	if !strings.Contains(lines[0], `"capabilities":{"tools":{}}`) {
		t.Errorf("initialize missing capabilities: %s", lines[0])
	}
	if !strings.Contains(lines[0], `"serverInfo":{"name":"vault","version":"0.1.0"}`) {
		t.Errorf("initialize missing serverInfo: %s", lines[0])
	}

	// (c) tools/list contains exactly the 3 tool names.
	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	if len(list.Result.Tools) != 3 {
		t.Errorf("(c) tools/list has %d tools, want exactly 3", len(list.Result.Tools))
	}
	got := map[string]bool{}
	for _, tool := range list.Result.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"report_activity", "check_context_health", "create_handoff"} {
		if !got[want] {
			t.Errorf("(c) tools/list missing tool %q", want)
		}
	}

	// tools/call report_activity returns "recorded #N".
	if !strings.Contains(lines[2], "recorded #1") {
		t.Errorf("report_activity result wrong: %s", lines[2])
	}
	// tools/call create_handoff returns the handoff path.
	if !strings.Contains(lines[3], "handoff_state.md") {
		t.Errorf("create_handoff result wrong: %s", lines[3])
	}
}

// TestInitializeDefaultProtocolVersion covers params:{} — protocolVersion
// falls back to "2025-06-18".
func TestInitializeDefaultProtocolVersion(t *testing.T) {
	out := runServer(t, t.TempDir(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n")
	lines := responseLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], `"protocolVersion":"2025-06-18"`) {
		t.Errorf("missing default protocolVersion: %s", lines[0])
	}
}

// TestPingResultEmptyObject asserts ping serializes as {"result":{}} — never
// null — and that a string id ("abc") is echoed raw.
func TestPingResultEmptyObject(t *testing.T) {
	out := runServer(t, t.TempDir(),
		`{"jsonrpc":"2.0","id":"abc","method":"ping"}`+"\n")
	lines := responseLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %q", len(lines), out)
	}
	want := `{"jsonrpc":"2.0","id":"abc","result":{}}`
	if lines[0] != want {
		t.Errorf("ping response = %s, want exactly %s", lines[0], want)
	}
	if strings.Contains(lines[0], `"result":null`) {
		t.Errorf("ping result must never be null: %s", lines[0])
	}
}

// TestErrorCases is table-driven: each bad message must produce the expected
// error, and the server must keep processing afterwards (ping still answered).
func TestErrorCases(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantID    string
		wantCode  int  // JSON-RPC error code, 0 for tool-level failures
		wantIsErr bool // tools/call tool-level failure -> isError:true
	}{
		{
			name:     "malformed json",
			line:     `{this is not json`,
			wantID:   "null",
			wantCode: -32700,
		},
		{
			name:     "unknown method",
			line:     `{"jsonrpc":"2.0","id":7,"method":"no_such_method"}`,
			wantID:   "7",
			wantCode: -32601,
		},
		{
			name: "create_handoff missing goal",
			line: `{"jsonrpc":"2.0","id":8,"method":"tools/call",` +
				`"params":{"name":"create_handoff","arguments":{"next_action":"x"}}}`,
			wantID:    "8",
			wantIsErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.line + "\n" + `{"jsonrpc":"2.0","id":99,"method":"ping"}` + "\n"
			out := runServer(t, t.TempDir(), input)
			lines := responseLines(t, out)
			if len(lines) != 2 {
				t.Fatalf("got %d response lines, want 2 (server must keep serving): %q", len(lines), out)
			}
			var first struct {
				ID     json.RawMessage `json:"id"`
				Result *struct {
					IsError bool `json:"isError"`
				} `json:"result"`
				Error *struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
				t.Fatalf("response 0 is not valid JSON: %v (%q)", err, lines[0])
			}
			if string(first.ID) != tc.wantID {
				t.Errorf("id = %s, want %s (raw id echo)", first.ID, tc.wantID)
			}
			if tc.wantIsErr {
				if first.Result == nil || !first.Result.IsError {
					t.Errorf("want isError:true (tool-level failure), got: %s", lines[0])
				}
			} else if first.Error == nil || first.Error.Code != tc.wantCode {
				t.Errorf("want JSON-RPC error %d, got: %s", tc.wantCode, lines[0])
			}
			if !strings.Contains(lines[1], `"result":{}`) {
				t.Errorf("server did not keep serving after error: %q", lines[1])
			}
		})
	}
}

// TestOversizedLine injects a small line limit: an oversized line must yield
// -32700 (id null), be discarded, and the server must keep serving.
func TestOversizedLine(t *testing.T) {
	var out bytes.Buffer
	srv := NewServer(nil, &out, t.TempDir())
	srv.log = log.New(io.Discard, "", 0)
	srv.maxLine = 64 // inject a small limit for the test (ping line is 43 bytes, so it fits)

	input := strings.Repeat("x", 200) + "\n" +
		`{"jsonrpc":"2.0","id":"abc","method":"ping"}` + "\n"
	srv.r = strings.NewReader(input)

	if err := srv.Serve(); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}
	lines := responseLines(t, out.String())
	if len(lines) != 2 {
		t.Fatalf("got %d response lines, want 2 (oversized + ping): %q", len(lines), out.String())
	}
	var first struct {
		ID    json.RawMessage `json:"id"`
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("oversized response not valid JSON: %v (%q)", err, lines[0])
	}
	if first.Error == nil || first.Error.Code != -32700 {
		t.Errorf("oversized line must yield -32700, got: %s", lines[0])
	}
	if string(first.ID) != "null" {
		t.Errorf("oversized line error id = %s, want null", first.ID)
	}
	if !strings.Contains(lines[1], `"result":{}`) {
		t.Errorf("server did not keep serving after oversized line: %q", lines[1])
	}
}
