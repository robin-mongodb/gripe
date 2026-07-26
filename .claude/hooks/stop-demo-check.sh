#!/usr/bin/env bash
# Stop hook: when the session ends and the working diff touches any demo-script step,
# flag which ones so we don't ship a change that breaks the 9-step demo.
#
# demo-verifier isn't built yet. Once it is, replace the echo with:
#   claude agent run demo-verifier --dry-run

set -euo pipefail

repo="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
cd "$repo"

changed="$(git diff --name-only)"
[ -z "$changed" ] && exit 0

declare -a hits
grep -qE 'seed|internal/domain|cmd/api'              <<<"$changed" && hits+=("step 1: seed / bootstrap")
grep -qE 'admin|list_all|employee'                    <<<"$changed" && hits+=("step 2: employee console list")
grep -qE 'merchant|list_merchant|dashboard'           <<<"$changed" && hits+=("step 3: merchant dashboard scoping")
grep -qE 'CreatePayment|payment_method|card|ach|bank_transfer|apple_pay|google_pay|direct_debit' \
                                                      <<<"$changed" && hits+=("step 4: create-payment across methods")
grep -qE 'idempoten'                                  <<<"$changed" && hits+=("step 5: idempotency")
grep -qE 'refund'                                     <<<"$changed" && hits+=("step 6: refund flow")
grep -qE 'subscription|cycler|DueSubscriptions'       <<<"$changed" && hits+=("step 7: subscription cycler")
grep -qE 'checkout|web/app/(pay|checkout)'            <<<"$changed" && hits+=("step 8: customer checkout")
grep -qE 'perf|k6|vegeta|locust'                      <<<"$changed" && hits+=("step 9: perf harness")

if [ ${#hits[@]} -gt 0 ]; then
  echo "[demo check] diff touches demo-script steps — verify before merging:"
  for h in "${hits[@]}"; do echo "  - $h"; done
fi
