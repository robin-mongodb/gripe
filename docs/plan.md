# Gripe — Build Approach

## Context

Two-week challenge to build the same payments platform twice — once on MongoDB, once on PostgreSQL (Aurora in prod, vanilla PG in tests) — then load-test both. Goal: a live demo that processes payments end-to-end on either backend, plus a perf report that stands up in a customer conversation.

Fraud detection, AI chat, and vector search are **parked** — reintroduced once the core payment flows are green.

This file is the pre-roadmap approach doc. The day-by-day roadmap is a separate deliverable.

## Personas

| # | Persona        | What they see / do                                                              |
|---|----------------|---------------------------------------------------------------------------------|
| 1 | Gripe employee | Every merchant, every payment; admin views; ops actions.                        |
| 2 | Merchant       | Their own payments; issue refunds; manage subscriptions.                        |
| 3 | Customer       | Checkout; pay a merchant; view/cancel their subscription. Built **last**.       |

Auth is skipped — actor identity is a header (`X-Actor-Role`, `X-Actor-Id`).

## Decisions locked in

| #   | Decision              | Choice                                                                                                                                                                                     |
| --- | --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | Core scope            | **Payments processing engine.** Create, capture, refund. Multiple payment methods. One-off + subscription.                                                                                 |
| 2   | Payment methods       | Card, direct debit (ACH/BACS-shaped), bank transfer, Apple Pay, Google Pay. All **mocked** — no PSP integration.                                                                           |
| 3   | Recurring             | Subscriptions with a configurable cadence; a background worker cycles due subscriptions and creates the next payment.                                                                      |
| 4   | Auth                  | **Skipped.** Actor in header (`X-Actor-Role: admin\|merchant\|customer`, `X-Actor-Id: <id>`).                                                                                              |
| 5   | Backend language      | **Go.** Single HTTP service + background worker (subscription cycler) sharing a Go module.                                                                                                 |
| 6   | Frontend              | **Next.js (App Router) + Tailwind.** Three surfaces: Gripe employee console, merchant dashboard, customer checkout (checkout is last).                                                     |
| 7   | Deploy target         | **Single EC2 + docker-compose.** Containers: `web`, `api`, `worker`, `postgres`. MongoDB on Atlas (separate). Prod PG would be **Aurora**; local/tests use vanilla PG (identical SQL).     |
| 8   | Repo shape            | **Single repo, swappable `Store` interface.** Two implementations: `store/mongo` and `store/postgres`. Config flag picks one at boot.                                                      |
| 9   | Idempotency           | `Idempotency-Key` header on payment + refund creates. Store persists the key → response mapping; duplicates return the original response.                                                  |
| 9a  | Gripe fee             | **3% flat on every payment**, applied on settlement. Merchant balance is credited `amount − fee`. On refund, balance is debited `refund_amount − refund_fee` (fee not returned to merchant). |
| 9b  | Currencies            | **USD, GBP, EUR only.** No FX. Merchant balance is per-currency; refund currency must equal payment currency; fee ledger split by currency.                                                |
| 10  | Perf tool             | **Deferred to week 2.** Any HTTP load tool (k6/locust/vegeta) works.                                                                                                                       |
| 11  | Seed data volume      | **Deferred.** Set once the backend is built and we can measure seed throughput.                                                                                                            |
| 12  | Parked capabilities   | Fraud worker, AI chat, vector search (Atlas Vector Search / `pgvector`), fuzzy search (Atlas Search / `pg_trgm` + `tsvector`), live event feed (Change Streams / `LISTEN/NOTIFY`).          |

## Approach — build shape

### Common code (write once)

- Next.js frontend (three surfaces)
- REST API + OpenAPI contract
- Domain DTOs + validation (payment, refund, subscription, payment method)
- `Store` interface (use-case-shaped)
- Payment method mocks (each method has its own auth/capture rules; e.g. bank transfer is async-settle)
- Idempotency middleware
- Seed generator (writes via `Store`)
- Contract tests (both backends, testcontainers)
- Subscription cycler worker
- Perf harness
- docker-compose

### Per-DB code (write twice)

| Concern              | MongoDB                                     | PostgreSQL (Aurora-compatible)                          |
| -------------------- | ------------------------------------------- | ------------------------------------------------------- |
| Schema               | Collections + documents                     | Tables + migrations (`goose` / `golang-migrate`)        |
| Idempotency store    | Unique index on `idempotency_key`           | Unique index on `idempotency_key`                       |
| Money math           | `Decimal128`                                | `numeric(19,4)` (never float)                           |
| Aggregations         | Aggregation pipeline (dashboards)           | SQL / CTE                                               |
| Indexes              | Compound (merchant, created_at, status)     | B-tree composite (merchant_id, created_at, status)      |
| Transactions         | Multi-document transactions where needed    | BEGIN/COMMIT                                            |

### Interface discipline

`Store` is use-case-shaped so each impl can be idiomatic:

- `CreatePayment(ctx, input, idempotencyKey)`
- `CapturePayment(ctx, paymentID)` — for methods with auth/capture separation
- `SettlePayment(ctx, paymentID)` — credits merchant balance by `amount − 3% fee` atomically with the state transition
- `RefundPayment(ctx, paymentID, amount, idempotencyKey)` — merchant chooses amount, `0 < amount ≤ remaining`
- `SettleRefund(ctx, refundID)` — debits merchant balance by `refund_amount − 3% fee` atomically
- `GetPayment(ctx, id, actor)` — actor-scoped read (admin sees all, merchant sees own, customer sees own)
- `ListMerchantPayments(ctx, merchantID, filters, cursor)`
- `ListAllPayments(ctx, filters, cursor)` — admin only
- `GetMerchantBalances(ctx, merchantID)` — returns `{USD: ..., GBP: ..., EUR: ...}` (only currencies with activity)
- `CreateSubscription(ctx, input)`
- `CancelSubscription(ctx, subscriptionID)`
- `DueSubscriptions(ctx, asOf, limit)` — worker pulls this

No `Find(collection, filter)` or `Query(sql, args)`.

## Build phasing (shape only — day-by-day is a separate doc)

| Phase              | Rough days | Output                                                                                                                                                          |
| ------------------ | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Skeleton           | 1–2        | Repo, docker-compose, Next.js shell, `Store` interface, `CreatePayment` + `GetPayment` against Mongo. Idempotency middleware.                                   |
| Mongo build        | 3–6        | All payment methods (mocked), refunds, subscriptions, cycler worker, merchant dashboard, admin console. Contract tests green.                                   |
| Postgres port      | 7–10       | PG schema + migrations, PG `Store` impl, same contract tests green. Aurora compatibility verified against a real Aurora instance if available; else vanilla PG. |
| Customer checkout  | 11         | Customer-facing pay surface. Same three payment method options end-to-end.                                                                                       |
| Perf               | 12–13      | Load tool of choice; scenarios covering create + list + refund. Run against both. Tune indexes. Comparison report.                                              |
| Buffer + demo      | 14         | README, demo script, TFW talking points.                                                                                                                        |

## Verification (how we'll know it's done)

Same demo script runs identically against both backends:

1. `make seed BACKEND=mongo|postgres` populates the chosen store with merchants, customers, and historical payments.
2. **Employee console:** admin sees all merchants and payments.
3. **Merchant dashboard:** merchant sees only their own payments.
4. **Create payment** via API for each method (card, direct debit, bank transfer, Apple Pay, Google Pay). Bank transfer settles async; the rest settle synchronously.
5. **Idempotency:** re-POST the same payment with the same `Idempotency-Key` — server returns the original response, does not create a duplicate.
6. **Refund** a captured payment via merchant dashboard. Merchant picks an amount between 0 and the remaining refundable. Partial + full refunds both work; over-refund is rejected.
6a. **Balance** for the merchant reflects `sum(settled) − sum(refunded) − 3% fee on each`; independent tally matches the store view to the cent, both backends.
7. **Subscription:** create one, run the cycler, observe the next payment being created on schedule.
8. **Customer checkout:** customer pays a merchant end-to-end.
9. **Perf run:** HTTP load tool exercises create + list + refund + cycler; reports p50/p95/p99, error rate, throughput for both DBs on the same scenario.

If all 9 steps pass on both backends and the perf report is legible, v1 is done.

## Deliverables

1. GitHub repo (single) with both `Store` implementations and both docker-compose profiles.
2. README with run instructions, feature callouts for TFW, and sample walkthrough scenarios.
3. Short performance report comparing Mongo vs PG on the same scenarios.
4. Note on "was one DB easier to build with Claude?" — captured while building, not retrofitted.

## Parked (revisit after v1)

- Fraud worker (agentic LLM + vector-similar past frauds via Voyage embeddings). Same shape both DBs.
- AI chat over payments history.
- Vector search (Atlas Vector Search / `pgvector` HNSW).
- Fuzzy search (Atlas Search / `pg_trgm` + `tsvector`).
- Live event feed (Change Streams / `LISTEN/NOTIFY`).
