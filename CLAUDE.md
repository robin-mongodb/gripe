# CLAUDE.md

## Project overview

**Gripe** is a Stripe-shaped payments platform built as a two-week SA challenge. Core: a payment processing engine that (mock-)accepts payments across methods (card, direct debit, bank transfer, Apple/Google Pay), issues refunds, and runs subscriptions — built twice, once on **MongoDB**, once on **PostgreSQL (Aurora in prod, vanilla PG in tests)** — then load-tested for a customer-facing perf comparison.

Full context is in `docs/plan.md`; treat it as the source of truth and read it before proposing structural changes. Deployed shape lives in `architecture.md` (visual mirror: `architecture.html`) — read that before proposing infra changes or new services.

## Personas

Three surfaces on top of the same backend:

1. **Gripe employee (admin)** — sees every merchant and every payment; ops / support.
2. **Merchant** — sees their own payments; can refund, manage subscriptions.
3. **Customer** — the checkout experience; built last.

## Architectural constraints that shape every file

- **Single repo, swappable `Store` interface.** Two impls: `store/mongo`, `store/postgres`. Config flag picks one at boot. Common code (API, DTOs, seed, contract tests, perf harness, frontend) is written once; only the `Store` impls are written twice.
- **`Store` is use-case-shaped, not CRUD-shaped.** Methods like `CreatePayment`, `RefundPayment`, `ListMerchantPayments`, `CycleSubscriptions`. **Do not** add `Find(collection, filter)` or `Query(sql, args)`.
- **Idempotency is a first-class concern.** Payment + refund endpoints accept an `Idempotency-Key`; the store rejects duplicates or returns the original result. Same contract on both backends.
- **Contract tests run against both backends** (via testcontainers). A feature isn't done until both are green.
- **Auth is skipped** — hardcoded actor context (admin / merchant ID / customer ID) in a header. Don't build auth.
- **Stack:** Go (api + fraud-worker + fee-worker sharing one module), Next.js App Router + Tailwind. Deploys as docker-compose on one EC2 (`nginx`, `web`, `api`, `fraud-worker`, `fee-worker`). **Aurora Postgres and MongoDB Atlas are managed** — no local DB containers in prod; testcontainers only for tests. **SQS** fans `payment.created` to both workers.

## Parked (future scope)

- **Fraud detection worker** (agentic LLM + vector-similar past frauds). Design preserved in `.claude/agents/fraud-worker-designer.md`. Do not build until core payment flows are green on both backends.
- **AI chat, Atlas Vector Search, `pgvector`.** Same reason.
- **Live event feed** (Change Streams / `LISTEN/NOTIFY`). Same reason.

## Use cases

Scope lives in `usecases.md`. Flow: use case → tasks in `tasks.html` → implementation. Don't skip the middle step — no code before the use case has rows in the task list.

## Tasks and progress

Tasks live **inside `tasks.html`** — the data is embedded in a `<script id="tasks-data" type="application/json">` block near the bottom of the file. That block is the single source of truth. Open the page directly in a browser (no server needed); edit the JSON block to add/update/complete tasks. Use-case bodies are embedded in the same file (`<script id="usecases-data">`).

**Every task row has a `paths` field.** When you build the task, populate `paths` with a comma-separated list of the files/dirs where the logic lives (e.g. `"store/mongo/payments.go, internal/domain/payment.go"`). Update it as the code moves. Empty string is fine until work starts. This is how future sessions locate context without re-scanning the repo.

## Testing

- **Contract tests are mandatory** — every `Store` feature must pass the shared contract suite against both Mongo and PG.
- **Write tests alongside non-trivial code:** branches, loops, parsers, money paths, idempotency logic. One runnable check that fails if the logic breaks.
- **Skip tests for trivial glue** (getters, wiring, one-line handlers). YAGNI applies to tests too.
- Update tests when the code under test changes.

## Comments

Explain non-obvious code with short comments. Prefer WHY over WHAT, but a one-line WHAT is fine when the code isn't self-evident (aggregation pipelines, index choices, idempotency logic). Skip on trivial getters, wiring, and obvious control flow.

## The "done" bar

Demo script in `docs/plan.md` (§ Verification) must run identically against both backends, plus a perf report with p50/p95/p99. If a change would make that script diverge between Mongo and PG, flag it.
