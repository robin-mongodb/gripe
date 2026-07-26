#!/usr/bin/env bash
# Stop hook: when the session ends and the working diff touches any demo-script step,
# run demo-verifier in dry-run so we don't ship a change that breaks the 7-step demo.
#
# demo-verifier isn't built yet (per user), so for now this hook just flags which
# demo steps are affected. Once the agent exists, replace the echo with:
#   claude agent run demo-verifier --dry-run

set -euo pipefail

repo="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
cd "$repo"

changed="$(git diff --name-only)"
[ -z "$changed" ] && exit 0

# demo steps → paths that would affect them (heuristic).
declare -a hits
grep -q 'store\|internal/domain\|cmd/api' <<<"$changed" && hits+=("step 1: seed / control-centre load")
grep -q 'SearchPayments\|store/mongo\|store/postgres' <<<"$changed" && hits+=("step 2: fuzzy search ranking")
grep -q 'FindSimilarPayments\|vector\|pgvector\|voyage' <<<"$changed" && hits+=("step 3: similar-cases retrieval")
grep -q 'worker/fraud\|fraud' <<<"$changed" && hits+=("step 4: fraud verdict + reasoning")
grep -q 'chat\|worker/fraud\|prompt' <<<"$changed" && hits+=("step 5: chat context retrieval")
grep -q 'SubscribeToPayment\|change[_-]stream\|LISTEN\|NOTIFY' <<<"$changed" && hits+=("step 6: live event feed")
grep -q 'perf\|k6\|vegeta' <<<"$changed" && hits+=("step 7: perf harness")

if [ ${#hits[@]} -gt 0 ]; then
  echo "[demo check] diff touches demo-script steps — run demo-verifier before merging:"
  for h in "${hits[@]}"; do echo "  - $h"; done
fi
