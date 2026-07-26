#!/usr/bin/env bash
# PostToolUse: after any edit under store/**, run the contract suite against both backends.
# Fires in the background so it doesn't block the agent; prints a summary line.

set -euo pipefail

payload="$(cat)"
file="$(printf '%s' "$payload" | jq -r '.tool_input.file_path // empty')"

case "$file" in
  */store/mongo/*|*/store/postgres/*|*/store/contract_test.go) ;;
  *) exit 0 ;;
esac

repo="$(git -C "$(dirname "$file")" rev-parse --show-toplevel 2>/dev/null || echo "")"
[ -z "$repo" ] && exit 0

# ponytail: run synchronously with a short timeout; upgrade to background if it slows the loop.
if ! go test -run Contract -count=1 -timeout 120s ./... >/tmp/gripe-contract.log 2>&1; then
  echo "[contract suite] FAIL — see /tmp/gripe-contract.log"
  exit 0  # non-blocking: warn, don't stop the agent mid-flow
fi
echo "[contract suite] PASS on both backends"
