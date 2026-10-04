#!/usr/bin/env bash
# Vault one-command installer.
#
#   make install          -> ./scripts/install.sh
#   make uninstall        -> ./scripts/install.sh --uninstall
#
# Idempotent: re-running only refreshes the "vault" MCP entry (after a
# backup) and never touches other servers. Set CLINE_MCP_SETTINGS to point at
# a specific settings file; otherwise common Linux/macOS locations are
# probed. Tests never invoke this script against real settings — the JSON
# merge itself is unit-tested in cmd/vault/mcp_register_test.go.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# detect_settings prints the path of Cline's MCP settings file: the first
# existing candidate on this platform, else the default CLI path (Linux:
# ~/.cline; macOS additionally probes the VS Code extension's globalStorage).
detect_settings() {
  local candidates=()
  if [ "$(uname -s)" = "Darwin" ]; then
    candidates+=(
      "$HOME/.cline/data/settings/cline_mcp_settings.json"
      "$HOME/Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json"
    )
  else
    candidates+=(
      "$HOME/.cline/data/settings/cline_mcp_settings.json"
      "$HOME/.config/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json"
    )
  fi
  for c in "${candidates[@]}"; do
    if [ -f "$c" ]; then
      echo "$c"
      return 0
    fi
  done
  echo "$HOME/.cline/data/settings/cline_mcp_settings.json"
}

# ---------------------------------------------------------------- uninstall
if [ "${1:-}" = "--uninstall" ]; then
  SETTINGS="${CLINE_MCP_SETTINGS:-$(detect_settings)}"
  go run ./cmd/vault mcp-register --unregister --settings "$SETTINGS"
  echo "Vault MCP entry removed. Restart Cline."
  exit 0
fi

# --------------------------------------------------------------- prereqs
if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: Go is not installed. Install Go 1.21+ from https://go.dev/dl/" >&2
  exit 1
fi
GOVER="$(go version | awk '{print $3}' | sed 's/^go//')"
if ! printf '%s\n' "1.21" "$GOVER" | sort -V -C; then
  echo "ERROR: Go >= 1.21 is required (found go$GOVER)." >&2
  exit 1
fi
if ! command -v git >/dev/null 2>&1; then
  echo "ERROR: git is not installed." >&2
  exit 1
fi

# ---------------------------------------------------------------- build
echo "==> building bin/vault"
go build -o bin/vault ./cmd/vault

# ------------------------------------------------------------- MCP entry
SETTINGS="${CLINE_MCP_SETTINGS:-$(detect_settings)}"
echo "==> registering vault MCP server in $SETTINGS"
./bin/vault mcp-register --settings "$SETTINGS" --root "$ROOT"

# ------------------------------------------------------- plugin deps (opt.)
if command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
  echo "==> installing telemetry plugin dependencies (npm ci)"
  if ! (cd .cline/plugins/vault-telemetry && npm ci --silent); then
    echo "WARN: npm ci failed — the telemetry plugin is optional and will be skipped."
  fi
else
  echo "NOTE: node/npm not found — skipping telemetry plugin dependencies (optional)."
fi

# ------------------------------------------------------ .clinerules hook
HOOK='After any failing command, call check_context_health. If the verdict is DEGRADED, stop and call create_handoff.'
if ! grep -qF 'If the verdict is DEGRADED, stop and call create_handoff.' .clinerules; then
  echo "$HOOK" >> .clinerules
  echo "==> added context-health hook to .clinerules"
else
  echo "==> .clinerules hook already present"
fi

# ------------------------------------------------------------- smoke test
echo "==> smoke test: bin/vault health"
./bin/vault health

echo
echo "Vault installed. Restart Cline."
