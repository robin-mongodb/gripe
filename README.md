# Gripe

A Stripe-shaped payments platform built twice — once on **MongoDB**, once on **PostgreSQL** —
behind a single use-case-shaped `Store` interface, then load-tested for a Mongo-vs-PG
performance comparison. Two-week SA challenge; see `docs/plan.md` for full context and
`architecture.md` for the deployed shape.

## What it does

- **Payments** across five (mock) methods: card, Apple Pay, Google Pay, direct debit, bank
  transfer. Card/wallets settle synchronously; direct debit lands authorized, bank transfer
  lands pending. Amounts ending in `.13` are declined.
- **Refunds** — partial and full, over-refund rejected, currency pinned to the payment.
- **Settlement** — atomic per-currency merchant balance credit `(amount − 3% fee)`, fee
  revenue accrual; refund settle returns Gripe's cut.
- **Subscriptions** — daily/weekly/monthly cadence; a cycler worker creates each cycle's
  payment idempotently.
- **Idempotency** everywhere it counts: `Idempotency-Key` on all mutating endpoints, same
  contract on both backends.
- **Three surfaces** (Next.js): admin console (volume, balances, Gripe revenue), merchant
  dashboard (payments, refunds, per-currency balances), customer checkout.

Auth is deliberately skipped — the actor is whatever `X-Actor-Role` / `X-Actor-Id` headers say.

## The point: one API, two databases

```
internal/store/store.go     ← use-case-shaped interface (no generic Find/Query)
store/mongo/                ← MongoDB implementation (Atlas in prod)
store/postgres/             ← PostgreSQL implementation (RDS in prod, goose migrations)
store/contract/             ← ONE contract test suite both impls must pass
```

`GRIPE_BACKEND=mongo|postgres` picks the implementation at boot. Everything else — API,
domain, seed, frontend, perf harness — is written once.

## Run it

Prereqs: Docker + a MongoDB connection string and/or a PostgreSQL DSN (managed DBs; no local
DB containers).

```sh
cp .env.example .env        # fill in MONGO_URI / PG_WRITER_DSN, pick GRIPE_BACKEND
docker compose up -d --build
open http://localhost       # nginx: / → web, /v1/* → api
docker compose run --rm seed -m 5 -p 40 -s 3   # demo data (mer_seed_000 …)
```

Flip backends by editing `GRIPE_BACKEND` in `.env` and `docker compose up -d` again.

## Tests

```sh
go test ./...               # unit + BOTH contract suites (needs Docker for testcontainers)
```

A `Store` feature is done only when the shared contract suite is green on Mongo **and** PG.

## Deploy

Terraform in `deploy/terraform/`: one EC2 (docker-compose via user-data), RDS PostgreSQL,
SQS queues, and an optional k6 load-generator box (`enable_loadgen = true`). Atlas is
user-owned; its URI goes in `terraform.tfvars` → `app_env`. See `deploy/terraform/README.md`.

## Perf comparison

`perf/` holds the k6 scenarios (create / list / refund flow / subscription churn) and the
run book for capturing p50/p95/p99 against each backend. Results feed the comparison report.

## Demo

`docs/demo.md` walks the nine verification steps from `docs/plan.md` — the same script must
pass identically against both backends.
