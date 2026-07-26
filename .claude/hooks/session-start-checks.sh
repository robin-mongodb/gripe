#!/usr/bin/env bash
# SessionStart: warn if usecases.md is missing or tasks.csv has rows without a matching use case.

set -euo pipefail

repo="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
cd "$repo"

warn=()

[ ! -f usecases.md ] && warn+=("usecases.md is missing — CLAUDE.md expects it.")

if [ -f usecases.md ] && [ -f tasks.csv ]; then
  # dumb heuristic: each usecase heading (## …) should appear as a substring in at least one task title.
  # ponytail: substring match, upgrade to explicit usecase-id column if this gets noisy.
  missing=0
  while IFS= read -r heading; do
    slug="$(printf '%s' "$heading" | sed 's/^## *//; s/[[:space:]]*$//')"
    [ -z "$slug" ] && continue
    if ! grep -qiF "$slug" tasks.csv; then
      warn+=("Usecase not in tasks.csv: $slug")
      missing=$((missing+1))
      [ "$missing" -ge 5 ] && { warn+=("…and more"); break; }
    fi
  done < <(grep -E '^## ' usecases.md || true)
fi

if [ ${#warn[@]} -gt 0 ]; then
  printf '[gripe session] warnings:\n'
  for w in "${warn[@]}"; do printf '  - %s\n' "$w"; done
fi
