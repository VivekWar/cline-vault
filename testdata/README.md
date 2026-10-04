# testdata

This directory holds **fixture inputs** for Vault's table-driven unit tests:

- **Fixture stderr logs** — captured agent/tool stderr streams used to feed
  the context-rot detection heuristics (repeated content, revision loops,
  tool-error storms, etc.).
- **Sample JSON-RPC transcripts** — realistic MCP over stdio conversations
  (requests, responses, notifications such as `initialized`) used to test
  protocol handling.

Conventions:
- Fixtures are plain files checked into git; keep them small and focused.
- Tests read them via `os.ReadFile("testdata/...")` or `go:embed`.
- Fixtures must be deterministic and offline-safe (no live captures with
  secrets, timestamps, or machine-specific paths).
