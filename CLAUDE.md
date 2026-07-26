# CLAUDE.md

## Project overview

**Gripe** is a Stripe-shaped payments platform built as a two-week SA challenge: the same payments **Control Centre + AI chat** use case, built twice — once on **MongoDB**, once on **PostgreSQL** — then load-tested for a customer-facing perf comparison.

Full context is in `docs/plan.md`; treat it as the source of truth and read it before proposing structural changes.

## Architectural constraints that shape every file

- **Single repo, swappable `Store` interface.** Two impls: `store/mongo`, `store/postgres`. Config flag picks one at boot. Common code (API, DTOs, seed, contract tests, perf harness, frontend) is written once; only the `Store` impls are written twice.
- **`Store` is use-case-shaped, not CRUD-shaped.** Methods like `SearchPayments`, `FindSimilarPayments`, `GetPaymentTimeline`, `SubscribeToPaymentEvents`. **Do not** add `Find(collection, filter)` or `Query(sql, args)` — the interface hides _how_, so each impl can be idiomatic (Aggregation pipeline vs SQL/CTE, Change Streams vs `LISTEN/NOTIFY`, Atlas Vector Search vs `pgvector`, Atlas Search vs `pg_trgm`+`tsvector`).
- **Contract tests run against both backends** (via testcontainers). A feature isn't done until both are green.
- **Fraud detection is agentic and async.** Worker pulls context (recent txns, geo, vector-similar past frauds), calls a small LLM, returns `{is_fraud, score, reasoning}`. Never on the write path. Same shape for both DBs.
- **Auth is skipped** — hardcoded merchant/support-user in a header. Don't build auth.
- **Stack:** Go (single HTTP service + background worker sharing a module), Next.js App Router + Tailwind, Voyage embeddings. Deploys as docker-compose on one EC2 (`web`, `api`, `worker`, `postgres`; MongoDB on Atlas). nginx routes `/` → web, `/v1/*` → api.

## Use cases

Scope lives in `usecases.md`. Flow: use case → tasks in `tasks.csv` → implementation. Don't skip the middle step — no code before the use case has rows in `tasks.csv`.

## Tasks and progress

Tasks live in `tasks.csv`; `tasks.html` renders them (open via `python3 -m http.server`, not `file://`). When a feature ships or scope changes, update `tasks.csv` — that's the single source of truth for progress.

## Testing

- **Contract tests are mandatory** — every `Store` feature must pass the shared contract suite against both Mongo and PG.
- **Write tests alongside non-trivial code:** branches, loops, parsers, money paths, fraud logic. One runnable check that fails if the logic breaks.
- **Skip tests for trivial glue** (getters, wiring, one-line handlers). YAGNI applies to tests too.
- Update tests when the code under test changes.

## Comments

Explain non-obvious code with short comments. Prefer WHY over WHAT, but a one-line WHAT is fine when the code isn't self-evident (aggregation pipelines, index choices,
LLM prompt shape). Skip on trivial getters, wiring, and obvious control flow.

## The "done" bar

Seven-step demo script in `docs/plan.md` (§ Verification) must run identically against both backends, plus a perf report with p50/p95/p99. If a change would make that script diverge between Mongo and PG, flag it.
