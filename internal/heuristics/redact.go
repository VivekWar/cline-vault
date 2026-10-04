package heuristics

import "regexp"

// Redact replaces common secrets in text with [REDACTED] so activity logs and
// handoff state never persist credentials. It is a pure, local, deterministic
// pass over four classes of secrets:
//
//   - API keys: sk- followed by 20+ alphanumeric characters
//   - bearer tokens: "Bearer <token>" (case-insensitive; the scheme word is
//     kept, only the token is masked)
//   - PEM private key blocks: -----BEGIN ... PRIVATE KEY----- through
//     -----END ... PRIVATE KEY----- (entire block masked)
//   - KEY=value pairs whose key contains KEY, TOKEN, SECRET or PASSWORD
//     (case-insensitive; the key is kept, only the value is masked)
//
// Everything else passes through unchanged. The KEY/TOKEN/SECRET/PASSWORD
// match is intentionally substring-based per the spec, so an identifier like
// "hockey" (contains "key") is treated as a secret name; that is the accepted
// false-positive trade-off for never leaking credentials.
func Redact(text string) string {
	text = privateKeyRe.ReplaceAllString(text, "[REDACTED]")
	text = bearerRe.ReplaceAllString(text, "$1[REDACTED]")
	text = apiKeyRe.ReplaceAllString(text, "[REDACTED]")
	text = keyValueRe.ReplaceAllString(text, "$1[REDACTED]")
	return text
}

var (
	// privateKeyRe matches a full PEM private key block, e.g.
	// -----BEGIN RSA PRIVATE KEY----- ... -----END RSA PRIVATE KEY-----.
	privateKeyRe = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)

	// bearerRe matches "Bearer <token>"; capture group 1 keeps the scheme word.
	bearerRe = regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]+`)

	// apiKeyRe matches OpenAI-style API keys (sk- prefix, 20+ chars).
	apiKeyRe = regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)

	// keyValueRe matches KEY=value pairs whose key contains KEY, TOKEN,
	// SECRET or PASSWORD; capture group 1 keeps the key and the "=" sign.
	keyValueRe = regexp.MustCompile(`(?i)(\b[\w.-]*(?:key|token|secret|password)[\w.-]*\s*=\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
)
