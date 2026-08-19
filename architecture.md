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

- **MongoDB Atlas** — M30, 3 nodes, eu-west-2, `w:majority`. Auth is **MONGODB-AWS (IAM)**: the EC2 instance role is an Atlas database user, so no password lives in `.env`. Reached via the public endpoint (~2.5–3.5 ms TCP RTT from the app box; in-region AWS backbone). `mongodb-atlas-local` used only in testcontainers (contract tests).
- **RDS PostgreSQL** — a **Multi-AZ DB cluster**: 3× `db.m6gd.large`, gp3 100 GiB (3,000 IOPS baseline), semi-synchronous commit (≈ `w:majority` — 2-of-3 semantics on both backends). Not Aurora. Both DSNs point at the **writer** endpoint for read parity with Mongo primary reads. In-VPC (~1 ms RTT). Vanilla PG only in testcontainers. Provisioned by `deploy/terraform/rds.tf`.
- **Amazon SQS** — one standard queue per event type. Currently: `payment.created`. Fanned to fraud-worker + fee-worker via two subscriptions (or SNS → 2 SQS; TBD when we wire it).

Neither backend runs on the EC2 host — DB load hits AWS/Atlas over the network, which is the load-comparison story.

## Perf load generator (perf phase only)

A second EC2 (`gripe-loadgen`, `t3.medium`, `deploy/terraform/loadgen.tf`, off by default via
`enable_loadgen`) runs k6 against the app box's **api port 8080 directly** — bypassing nginx,
whose 100 r/s rate limit would dominate the measurement. Separate machine so the load generator
never steals CPU from the system under test; same subnet so the client→api hop is constant and
the only variable is the database. Scenarios live in `perf/scenarios.js`; run them via
`perf/run-perf.sh <postgres|mongo> [RATE] [DURATION]`, which persists every k6 summary to Atlas
`gripe_perf.results` (mongosh + the shared IAM role) so results survive the EC2 cleanup reaper.

## Perf outcome (2026-08-19)

Both backends held 368 req/s for 10 min with zero errors; after task-42 tuning (covered list
index, onboarding cache, `maxPoolSize` 200) Mongo won the tails (create p95 26 ms vs 69 ms),
PG kept the read p50 edge (network path). Full report: `docs/perf-report.html` (+ PNGs in
`docs/perf/`); data-model comparison: `docs/data-models.html`; DX note: `docs/claude-dx.md`.

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

| Concern                       | Path                                                               |
| ----------------------------- | ------------------------------------------------------------------ |
| Store interface + DTOs        | `internal/store/`, `internal/domain/`                              |
| Mongo impl                    | `store/mongo/`                                                     |
| Postgres impl + migrations    | `store/postgres/`, `store/postgres/migrations/`                    |
| Contract tests + testcontainers | `store/contract/`, `store/*/contract_test.go`                    |
| API handlers                  | `cmd/api/`, `internal/api/`                                        |
| Workers                       | `cmd/{fraud-worker,fee-worker,cycler}/`, `internal/workers/`, `internal/cycler/` |
| Frontend                      | `web/app/(admin\|merchant\|checkout)/`                             |
| Perf harness + report         | `perf/`, `docs/perf-report.html`, `docs/perf/`                     |
| Infra                         | `docker-compose.yml`, `deploy/terraform/`, `deploy/nginx/`         |

Update `paths` on task rows in `tasks.html` as code lands.
