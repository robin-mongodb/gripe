# Demo script — the nine verification steps

From `docs/plan.md` § Verification. Run the whole script twice: once with
`GRIPE_BACKEND=mongo`, once with `GRIPE_BACKEND=postgres` (edit `.env`,
`docker compose up -d`). Every step must behave identically.

Set up once per backend:

```sh
BASE=http://localhost/v1          # or http://<app_public_ip>/v1 on the demo box
docker compose run --rm seed -m 5 -p 40 -s 3
```

## 1. Seed

The seed command above populates merchants (`mer_seed_000`…`mer_seed_004`), customers,
payments (~60% of settleable ones settled), and subscriptions. It prints a report with counts.

## 2. Employee console — admin sees everything

Open `/admin`: Gripe revenue split by currency, per-merchant balances, volume per merchant
per day. Or via API:

```sh
curl -H "X-Actor-Role: admin" -H "X-Actor-Id: admin" "$BASE/payments" | head -c 400
curl -H "X-Actor-Role: admin" -H "X-Actor-Id: admin" "$BASE/reports/volume"
```

## 3. Merchant dashboard — own payments only

Open `/merchant` (acting as `mer_seed_000`). Or prove the scoping:

```sh
curl -H "X-Actor-Role: merchant" -H "X-Actor-Id: mer_seed_000" "$BASE/payments"
# every row has merchant_id == mer_seed_000
```

## 4. Create a payment per method

```sh
for M in card apple_pay google_pay direct_debit bank_transfer; do
  curl -s -X POST -H "X-Actor-Role: customer" -H "X-Actor-Id: cus_demo" \
    -H "Idempotency-Key: demo-$M" -H "Content-Type: application/json" \
    -d '{"merchant_id":"mer_seed_000","customer_id":"cus_demo","amount_minor":5000,"currency":"GBP","method":"'$M'"}' \
    "$BASE/payments" | python3 -c 'import json,sys; p=json.load(sys.stdin); print(p["method"], "->", p["status"])'
done
# card/apple_pay/google_pay -> captured, direct_debit -> authorized, bank_transfer -> pending
```

## 5. Idempotency

Re-POST any of the above with the same `Idempotency-Key`: the original response comes back,
no duplicate is created (payment count unchanged in the dashboard).

## 6. Refund

From the merchant dashboard: click a captured payment → refund part of it → refund again for
the rest → a third attempt is rejected (over-refund, HTTP 422). Amounts ending `.13` decline
at create and cannot be refunded.

## 6a. Balance invariant

```sh
curl -H "X-Actor-Role: merchant" -H "X-Actor-Id: mer_seed_000" "$BASE/balances"
```

For each currency: `balance_minor == Σ settled payments − Σ settled refunds − 3% fee on each`
(fees round half-even). Settle a payment as admin and watch the balance move by exactly
`amount − fee`:

```sh
curl -X POST -H "X-Actor-Role: admin" -H "X-Actor-Id: admin" "$BASE/payments/<id>/settle"
```

## 7. Subscription cycle

Create a subscription with a past start, then watch the cycler (runs in compose) create the
cycle's payment:

```sh
curl -X POST -H "X-Actor-Role: merchant" -H "X-Actor-Id: mer_seed_000" \
  -H "Idempotency-Key: demo-sub-1" -H "Content-Type: application/json" \
  -d '{"merchant_id":"mer_seed_000","customer_id":"cus_demo","amount_minor":900,"currency":"USD","method":"card","cadence":"daily","start_at":"2026-01-01T00:00:00Z"}' \
  "$BASE/subscriptions"
docker compose logs cycler --since 2m   # shows the charge being created, once per cycle_index
```

## 8. Customer checkout

Open `/checkout`, pick a method, pay `mer_seed_000` — success page for captured/pending,
failure page for a `.13` amount. The payment appears in the merchant dashboard immediately.

## 9. Perf run

See `perf/README.md` — k6 scenarios against each backend from the loadgen box; capture
p50/p95/p99, error rate, throughput.
