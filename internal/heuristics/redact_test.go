package heuristics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedact covers each secret class plus the pass-through and idempotence
// guarantees, table-driven.
func TestRedact(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "sk api key",
			in:   `key: sk-abcdefghijklmnopqrstuvwxyz123`,
			want: `key: [REDACTED]`,
		},
		{
			name: "short sk-like token untouched",
			in:   `token: sk-short`,
			want: `token: sk-short`,
		},
		{
			name: "bearer token",
			in:   `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature`,
			want: `Authorization: Bearer [REDACTED]`,
		},
		{
			name: "lowercase bearer",
			in:   `x-bearer: bearer abcDEF123._~+/-=`,
			want: `x-bearer: bearer [REDACTED]`,
		},
		{
			name: "private key block",
			in:   "creds:\n-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASC\n-----END PRIVATE KEY-----\nrest of the log",
			want: "creds:\n[REDACTED]\nrest of the log",
		},
		{
			name: "rsa private key block",
			in:   "-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----",
			want: "[REDACTED]",
		},
		{
			name: "key=value pairs",
			in:   `AWS_SECRET_ACCESS_KEY=abcd1234 PASSWORD=hunter2 api_token=xyzzy DB_PASSWORD="quoted value"`,
			want: `AWS_SECRET_ACCESS_KEY=[REDACTED] PASSWORD=[REDACTED] api_token=[REDACTED] DB_PASSWORD=[REDACTED]`,
		},
		{
			name: "benign text untouched",
			in:   `status: ok\nfiles: a.go b.go\nPASS`,
			want: `status: ok\nfiles: a.go b.go\nPASS`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.in)
			if got != tc.want {
				t.Errorf("Redact(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
			if again := Redact(got); again != got {
				t.Errorf("Redact not idempotent: %q -> %q", got, again)
			}
		})
	}
}

// TestRedactFixture runs Redact over a fixture stderr log carrying all four
// secret classes at once and asserts every raw secret is gone.
func TestRedactFixture(t *testing.T) {
	fixture := fixtureFile(t, "redact_fixture.txt")
	got := Redact(fixture)
	rawSecrets := []string{
		"sk-4jf8w2hYpQdLmN7vXc3gKtR9uEa5z",
		"eyJhbGciOiJIUzI1NiJ9.part2.part3",
		"-----BEGIN PRIVATE KEY-----",
		"m9qLrTzKw8xVpQ2nB5cD7fGhJ4kM6uS",
	}
	for _, s := range rawSecrets {
		if strings.Contains(got, s) {
			t.Errorf("raw secret leaked after Redact: %q", s)
		}
	}
	if n := strings.Count(got, "[REDACTED]"); n != 4 {
		t.Errorf("got %d [REDACTED] markers, want 4:\n%s", n, got)
	}
	if !strings.Contains(got, "API_TOKEN=[REDACTED]") {
		t.Errorf("expected API_TOKEN value masked:\n%s", got)
	}
}

// fixtureFile returns the content of testdata/heuristics/<name> relative to
// the module root (mirrors the state package's fixture helper so heuristics
// tests can share fixtures without importing state).
func fixtureFile(t *testing.T, name string) string {
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
