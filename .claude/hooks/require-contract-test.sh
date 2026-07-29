#!/usr/bin/env bash
# PreToolUse: block edits to store/** that don't touch a matching contract test.
# Input: JSON on stdin from Claude Code with tool_input.file_path.
# Exit 0 = allow, non-zero = block with the printed message.

set -euo pipefail

payload="$(cat)"
file="$(printf '%s' "$payload" | jq -r '.tool_input.file_path // empty')"

# only care about edits under store/
case "$file" in
  */store/mongo/*|*/store/postgres/*) ;;
  *) exit 0 ;;
esac

# skip if we're editing the contract test itself, or a testcontainers helper
case "$file" in
  */contract_test.go|*/testsupport/*) exit 0 ;;
esac

# check contract_test.go is staged/modified in this session — from repo root so
# a shared store/contract_test.go is visible even when editing store/mongo/*.
repo="$(git -C "$(dirname "$file")" rev-parse --show-toplevel 2>/dev/null || echo "")"
if [ -z "$repo" ]; then exit 0; fi

if ! git -C "$repo" status --porcelain 2>/dev/null | grep -qE 'store/(contract/contract\.go|contract_test\.go|[a-z]+/contract_test\.go)'; then
  echo "Blocked: editing $file requires a matching change in the shared contract suite" >&2
  echo "(store/contract/contract.go or a backend contract_test.go)." >&2
  echo "A Store feature isn't done until both backends pass the same contract." >&2
  exit 2
fi
