package heuristics

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from cwd")
		}
		dir = parent
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "heuristics", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "paths stripped",
			in:   "/abs/path/calc_test.go:12:34 rel/calc.go:5 main.go got 2 want 3",
			want: []string{"got", "want"},
		},
		{
			name: "hex addresses stripped",
			in:   "addr=0x7ffc9a5f pc=0x0 value",
			want: []string{"addr", "pc", "value"},
		},
		{
			name: "rfc3339 timestamps stripped",
			in:   "2024-10-04T12:34:56Z at 2024-10-04T12:34:56.789+00:00 now",
			want: []string{"at", "now"},
		},
		{
			name: "clock timestamps stripped",
			in:   "12:34:56 hello",
			want: []string{"hello"},
		},
		{
			name: "durations stripped",
			in:   "0.123s 12ms 1.5h go build ok",
			want: []string{"build", "go", "ok"},
		},
		{
			name: "bare numbers stripped",
			in:   "42 apples 7",
			want: []string{"apples"},
		},
		{
			name: "go test boilerplate stripped",
			in:   "=== RUN\n--- PASS\nPASS\nFAIL\nok  \texample.com/pkg\t0.123s\nexit status 1\nFAIL\texample.com/pkg\nreal token\n",
			want: []string{"real", "token"},
		},
		{
			name: "lowercase dedup short tokens",
			in:   "A b Foo foo FOO bar",
			want: []string{"bar", "foo"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.in)
			if !equalStrings(got, tc.want) {
				t.Errorf("Normalize(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestJaccard(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want float64
	}{
		{"identical", []string{"a", "b", "c"}, []string{"a", "b", "c"}, 1.0},
		{"disjoint", []string{"a", "b"}, []string{"c", "d"}, 0.0},
		{"both empty", []string{}, []string{}, 0.0},
		{"one empty", []string{"a"}, []string{}, 0.0},
		{"partial", []string{"a", "b", "c"}, []string{"b", "c", "d"}, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Jaccard(tc.a, tc.b)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("Jaccard(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestDetectErrorLoop(t *testing.T) {
	loop1 := fixture(t, "loop_same_assertion_1.txt")
	loop2 := fixture(t, "loop_same_assertion_2.txt")
	loop3 := fixture(t, "loop_same_assertion_3.txt")
	p1 := fixture(t, "progress_1.txt")
	p2 := fixture(t, "progress_2.txt")
	p3 := fixture(t, "progress_3.txt")

	cases := []struct {
		name     string
		entries  []Entry
		wantFlag bool
	}{
		{
			name: "loop fixtures flag",
			entries: []Entry{
				{Kind: "TEST", ExitCode: 1, Output: loop1},
				{Kind: "TEST", ExitCode: 1, Output: loop2},
				{Kind: "TEST", ExitCode: 1, Output: loop3},
			},
			wantFlag: true,
		},
		{
			name: "progress fixtures no flag",
			entries: []Entry{
				{Kind: "TEST", ExitCode: 1, Output: p1},
				{Kind: "TEST", ExitCode: 1, Output: p2},
				{Kind: "TEST", ExitCode: 1, Output: p3},
			},
			wantFlag: false,
		},
		{
			name: "success in the middle breaks the loop",
			entries: []Entry{
				{Kind: "TEST", ExitCode: 1, Output: loop1},
				{Kind: "TEST", ExitCode: 1, Output: loop2},
				{Kind: "TEST", ExitCode: 0, Output: loop3},
				{Kind: "TEST", ExitCode: 1, Output: loop1},
			},
			wantFlag: false,
		},
		{
			name: "only COMMAND and TEST are considered",
			entries: []Entry{
				{Kind: "EDIT", ExitCode: 1, Output: loop1},
				{Kind: "TEST", ExitCode: 1, Output: loop1},
				{Kind: "TEST", ExitCode: 1, Output: loop2},
				{Kind: "TEST", ExitCode: 1, Output: loop3},
			},
			wantFlag: true,
		},
		{
			name: "fewer than 3 relevant entries",
			entries: []Entry{
				{Kind: "TEST", ExitCode: 1, Output: loop1},
				{Kind: "TEST", ExitCode: 1, Output: loop2},
			},
			wantFlag: false,
		},
		{
			name: "empty output among last 3",
			entries: []Entry{
				{Kind: "TEST", ExitCode: 1, Output: loop1},
				{Kind: "TEST", ExitCode: 1, Output: ""},
				{Kind: "TEST", ExitCode: 1, Output: loop3},
			},
			wantFlag: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectErrorLoop(tc.entries)
			if tc.wantFlag {
				if got == nil {
					t.Fatal("want RECURRING_ERROR_LOOP flag, got nil")
				}
				if got.Name != "RECURRING_ERROR_LOOP" {
					t.Errorf("name = %q", got.Name)
				}
				ev, ok := got.Evidence.(LoopEvidence)
				if !ok {
					t.Fatalf("evidence type = %T", got.Evidence)
				}
				if len(ev.Similarities) != 3 {
					t.Errorf("similarities len = %d, want 3", len(ev.Similarities))
				}
				for _, s := range ev.Similarities {
					if s < 0.70 {
						t.Errorf("similarity %v below threshold", s)
					}
				}
				if len(ev.SharedTokens) == 0 {
					t.Errorf("shared tokens empty")
				}
			} else if got != nil {
				t.Errorf("want no flag, got %+v", got)
			}
		})
	}
}

func TestParseNumstat(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantAdd    int
		wantDelete int
	}{
		{"normal", "10\t5\tf.go\n3\t0\tg.go\n", 13, 5},
		{"binary counts zero", "-\t-\timg.png\n2\t1\tf.go\n", 2, 1},
		{"empty", "", 0, 0},
		{"rename line", "0\t4\told.go => new.go\n", 0, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			add, del := parseNumstat(tc.in)
			if add != tc.wantAdd || del != tc.wantDelete {
				t.Errorf("parseNumstat(%q) = (%d, %d), want (%d, %d)", tc.in, add, del, tc.wantAdd, tc.wantDelete)
			}
		})
	}
}

func TestChurnEfficiency(t *testing.T) {
	cases := []struct {
		name       string
		net, gross float64
		want       float64
	}{
		{"normal", 30, 300, 0.1},
		{"gross zero", 0, 0, 0},
		{"gross zero with net", 5, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := churnEfficiency(tc.net, tc.gross); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("churnEfficiency(%v, %v) = %v, want %v", tc.net, tc.gross, got, tc.want)
			}
		})
	}
}

func TestDetectOscillation(t *testing.T) {
	cases := []struct {
		name       string
		net, gross float64
		snapshots  int
		wantFlag   bool
	}{
		{"below gross threshold", 0, 100, 3, false},
		{"low efficiency flags", 30, 300, 3, true},
		{"high efficiency no flag", 50, 300, 3, false},
		{"zero gross no flag", 0, 0, 2, false},
		{"micro oscillation flags", 0, 6, 12, true},
		{"micro oscillation small net", 2, 15, 15, true},
		{"micro oscillation below snapshot threshold", 0, 6, 9, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectOscillation(tc.net, tc.gross, tc.snapshots)
			if tc.wantFlag {
				if got == nil || got.Name != "CODE_OSCILLATION_THRASHING" {
					t.Fatalf("want CODE_OSCILLATION_THRASHING, got %+v", got)
				}
			} else if got != nil {
				t.Errorf("want no flag, got %+v", got)
			}
		})
	}
}

func TestAssess(t *testing.T) {
	loop1 := fixture(t, "loop_same_assertion_1.txt")
	loop2 := fixture(t, "loop_same_assertion_2.txt")
	loop3 := fixture(t, "loop_same_assertion_3.txt")
	loopEntries := []Entry{
		{Kind: "TEST", ExitCode: 1, Output: loop1},
		{Kind: "TEST", ExitCode: 1, Output: loop2},
		{Kind: "TEST", ExitCode: 1, Output: loop3},
	}
	oscillate := func(ws, a, b string) (string, error) {
		if a == "t1" && b == "t3" {
			return "0\t0\tf.txt\n", nil
		}
		return "100\t0\tf.txt\n", nil
	}

	t.Run("no flags", func(t *testing.T) {
		h := Assess("", []Entry{{Kind: "COMMAND", ExitCode: 0, Output: "ok"}}, nil)
		if h.Score != 100 || h.Status != "healthy" {
			t.Errorf("score=%d status=%s, want 100 healthy", h.Score, h.Status)
		}
		if h.Recommendation != "continue" {
			t.Errorf("recommendation = %q, want continue", h.Recommendation)
		}
		if len(h.Flags) != 0 {
			t.Errorf("flags = %v, want none", h.Flags)
		}
	})

	t.Run("loop flag degrades", func(t *testing.T) {
		h := Assess("", loopEntries, nil)
		if h.Score != 50 || h.Status != "degraded" {
			t.Errorf("score=%d status=%s, want 50 degraded", h.Score, h.Status)
		}
		if h.Recommendation != "call create_handoff and start a fresh task" {
			t.Errorf("recommendation = %q", h.Recommendation)
		}
	})

	t.Run("oscillation only", func(t *testing.T) {
		orig := diffNumstatFn
		diffNumstatFn = oscillate
		t.Cleanup(func() { diffNumstatFn = orig })
		h := Assess("", []Entry{{Kind: "COMMAND", ExitCode: 0, Output: "ok"}}, []string{"t1", "t2", "t3"})
		if h.Score != 65 || h.Status != "degraded" {
			t.Errorf("score=%d status=%s, want 65 degraded", h.Score, h.Status)
		}
		if h.Metrics.Gross != 200 || h.Metrics.Efficiency != 0 {
			t.Errorf("gross=%v efficiency=%v, want 200 / 0", h.Metrics.Gross, h.Metrics.Efficiency)
		}
	})

	t.Run("micro oscillation only", func(t *testing.T) {
		orig := diffNumstatFn
		diffNumstatFn = func(ws, a, b string) (string, error) {
			if a == "m1" && b == "m10" {
				return "0\t0\tf.txt\n", nil
			}
			return "1\t0\tf.txt\n", nil
		}
		t.Cleanup(func() { diffNumstatFn = orig })
		trees := []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7", "m8", "m9", "m10"}
		h := Assess("", []Entry{{Kind: "COMMAND", ExitCode: 0, Output: "ok"}}, trees)
		if h.Score != 65 || h.Status != "degraded" {
			t.Errorf("score=%d status=%s, want 65 degraded", h.Score, h.Status)
		}
		if h.Metrics.Gross != 9 || h.Metrics.Efficiency != 0 {
			t.Errorf("gross=%v efficiency=%v, want 9 / 0", h.Metrics.Gross, h.Metrics.Efficiency)
		}
	})

	t.Run("loop plus oscillation critical", func(t *testing.T) {
		orig := diffNumstatFn
		diffNumstatFn = oscillate
		t.Cleanup(func() { diffNumstatFn = orig })
		h := Assess("", loopEntries, []string{"t1", "t2", "t3"})
		if h.Score != 15 || h.Status != "critical" {
			t.Errorf("score=%d status=%s, want 15 critical", h.Score, h.Status)
		}
	})
}
