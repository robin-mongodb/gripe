#!/usr/bin/env bash
# PostToolUse: after any non-trivial edit, nudge the user to sync tasks.csv.
# Trivial = docs, .md, config, tests only. Real code changes trigger the nudge.

set -euo pipefail

payload="$(cat)"
file="$(printf '%s' "$payload" | jq -r '.tool_input.file_path // empty')"

[ -z "$file" ] && exit 0

case "$file" in
  *.md|*/docs/*|*/tasks.html|*/.claude/*|*/README*) exit 0 ;;
  *_test.go|*.test.ts|*.test.tsx|*.spec.ts) exit 0 ;;
esac

echo "[reminder] tasks list (in tasks.html) may need updating for: $file"
echo "           run the tasks-csv-sync skill when the change is stable."
