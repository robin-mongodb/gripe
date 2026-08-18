# Gripe — Architecture

Single source of truth for the deployed shape. `architecture.html` is the visual mirror — keep both in sync.

## Runtime

**Single EC2 host + docker-compose.** Containers:

| Container       | What it is                                                                                     |
| --------------- | ---------------------------------------------------------------------------------------------- |
| `nginx`         | Routes `/` → `web`, `/v1/*` → `api`. TLS termination.                                          |
| `web`           | Next.js App Router (Tailwind). Three surfaces: admin console, merchant dashboard, checkout.    |
| `api`           | Go HTTP service. Actor header middleware, idempotency, calls `Store`.                          |
| `fraud-worker`  | Go binary. **Idle stub** — fraud detection is parked (see CLAUDE.md); boots, logs, waits.       |
| `fee-worker`    | Go binary. Polls `payment.created` SQS. Mock network-fee calc → `Store.SettleNetworkFee` (set-once). |
| `cycler`        | Go binary. Polls the store for due subscriptions; creates the next payment per cycle. Idempotent per `(subscription_id, cycle_index)`. |

All Go binaries share one module (`gripe/`). `api` and workers use the same `Store` implementation chosen by env var.

Deploy target for workers may move to Fargate later — `docker-compose` for v1, no code changes required to lift.

## Managed services

- **MongoDB Atlas** — the Mongo `Store` impl talks to Atlas over the internet from EC2. `mongodb-atlas-local` used only in testcontainers (contract tests).
- **RDS PostgreSQL** — the PG `Store` impl targets a plain RDS PostgreSQL instance (single-AZ, `db.t4g.micro`). Not Aurora. Vanilla PG used only in testcontainers. Provisioned by `deploy/terraform/rds.tf`.
- **Amazon SQS** — one standard queue per event type. Currently: `payment.created`. Fanned to fraud-worker + fee-worker via two subscriptions (or SNS → 2 SQS; TBD when we wire it).

Neither backend runs on the EC2 host — DB load hits AWS/Atlas over the network, which is the load-comparison story.

## Perf load generator (perf phase only)

A second EC2 (`gripe-loadgen`, `t3.medium`, `deploy/terraform/loadgen.tf`, off by default via
`enable_loadgen`) runs k6 against the app box's **api port 8080 directly** — bypassing nginx,
whose 100 r/s rate limit would dominate the measurement. Separate machine so the load generator
never steals CPU from the system under test; same subnet so the client→api hop is constant and
the only variable is the database. Scenarios live in `perf/scenarios.js`.

## Data flow

1. **Create payment.** Client → `nginx` → `api` → `Store.CreatePayment(ctx, input, idempotencyKey)` → RDS PostgreSQL or Atlas.
2. **Publish event.** `api` writes a small `{payment_id, merchant_id, amount_minor, currency}` message to the `payment.created` queue on the write path, for non-declined payments (best-effort; failure is logged, never surfaced — `internal/events`).
3. **Async network fee.** `fee-worker` consumes `payment.created` directly (it's the only consumer while fraud is parked; SNS fan-out to the per-worker queues comes back with fraud) → `Store.SettleNetworkFee(paymentID, fee)` — a set-once conditional update, so SQS at-least-once redelivery is harmless.
4. **Async fraud.** Parked. `fraud-worker` boots and idles; `SettleFraudScore` returns with the fraud scope.
5. **Read side.** Merchant/admin dashboards read via `api` → `Store` → DB. No caching yet.

The two workers hit each DB with reads + updates, which mixes the workload beyond pure inserts — that's the point.

## Key architectural constraints

- **Store interface is use-case-shaped**, not CRUD-shaped (see CLAUDE.md).
- **Contract tests run against both backends** via testcontainers.
- **Idempotency** is a first-class concern on writes (payment create, refund, worker settle-* methods).
- **Money math** as `int64` minor units end-to-end in Go, stored as `int64` (Mongo) / `BIGINT` (PG). Never float; fees round half-even (`internal/domain/money.go`).
- **Currency-scoped balances** — `merchant_balances(merchant_id, currency)`; no FX.
- **Auth is skipped** — actor identity in `X-Actor-Role` + `X-Actor-Id` headers.

## Parked / future

- Change Streams (Mongo) and `LISTEN/NOTIFY` (PG) — earmarked for a **live event feed UI**, not as the worker trigger. Trigger is SQS to keep parity between the two DBs.
- Fraud "reasoning" via LLM (agentic) — kept simple for v1: score only. Reasoning + vector-similar past frauds return with the parked scope.
- AI chat, Atlas Vector Search, `pgvector`, fuzzy search — all parked.
- Move workers to Fargate.
- Payouts, disputes, FX, PSP integration.

## What lives where in the repo

| Concern                       | Path (planned)                                                     |
| ----------------------------- | ------------------------------------------------------------------ |
| Store interface + DTOs        | `internal/store/`, `internal/domain/`                              |
| Mongo impl                    | `store/mongo/`                                                     |
| Postgres impl + migrations    | `store/postgres/`, `store/postgres/migrations/`                    |
| Contract tests + testcontainers | `store/contract_test.go`, `store/testsupport/`                   |
| API handlers                  | `cmd/api/`, `internal/api/`                                        |
| Workers                       | `cmd/fraud-worker/`, `cmd/fee-worker/`, `internal/workers/`        |
| Frontend                      | `web/app/(admin\|merchant\|checkout)/`                             |
| Infra                         | `docker-compose.yml`, `deploy/ec2/`, `deploy/sqs/`                 |

Update `paths` on task rows in `tasks.html` as code lands.
