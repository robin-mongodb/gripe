# AwesomePayments — Build Approach (Modules 3 & 4)

## Context

Two-week challenge to build the same payments use case twice — once on MongoDB, once on PostgreSQL — then load-test both. Goal is a demo that shows where MongoDB features (Atlas Search, Vector Search, Change Streams, flexible schema) beat or match PG equivalents (`pg_trgm`+`tsvector`, `pgvector`, `LISTEN/NOTIFY`), and a perf report that stands up in a customer conversation.

This file is the pre-roadmap approach doc. The day-by-day roadmap is a separate deliverable, built after this is approved.

## Decisions locked in

| #   | Decision         | Choice                                                                                                                                                                                                                                       |
| --- | ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Use case         | **Payments Control Centre + AI chat.** Support worker traces failed/suspicious/missing payments, sees fraud flags, asks an AI agent "why did this fail?" / "show similar cases" over historical transactions.                                |
| 2   | AI scope         | **Full.** Voyage embeddings + Vector Search for "similar cases" retrieval, plus a chat panel over payments history.                                                                                                                          |
| 3   | Fraud signals    | **Agentic model.** Small LLM runs on the worker, pulls context (recent txns, geo, similar past frauds via vector search), returns `{is_fraud, score, reasoning}`. Async — does not block the write path. Same architecture for both DBs.     |
| 4   | Auth             | **Skipped.** Hardcoded merchant/support-user context in a header.                                                                                                                                                                            |
| 5   | Backend language | **Go.** Single HTTP service + background worker sharing a Go module.                                                                                                                                                                         |
| 6   | Frontend         | **Next.js (App Router) + Tailwind.** Lean scope: control centre screen (search + list + detail + similar cases + chat panel) + live event feed sidebar.                                                                                      |
| 7   | Deploy target    | **Single EC2 + docker-compose.** Containers: `web`, `api`, `worker`, `postgres`. MongoDB on Atlas (separate). nginx routes `/` → web, `/v1/*` → api. Perf tests hit the Go API directly, so frontend-on-same-box doesn't affect the numbers. |
| 8   | Repo shape       | **Single repo, swappable `Store` interface.** Two implementations: `store/mongo` and `store/postgres`. Config flag picks one at boot.                                                                                                        |
| 9   | PG change-events | **`LISTEN/NOTIFY`.** Native to PG. Weaker than Change Streams (no resume tokens, at-most-once) — honest comparison for the demo.                                                                                                             |
| 10  | Perf tool        | **Deferred to week 2.** Any HTTP load tool (k6/locust/vegeta) can drive the app; choice doesn't affect design.                                                                                                                               |
| 11  | Seed data volume | **Deferred.** Set once the backend is built and we can measure seed throughput and storage footprint on the demo box.                                                                                                                        |

## Approach — build shape

### Common code (write once)

- Next.js frontend
- REST API + OpenAPI contract
- Domain DTOs and validation
- `Store` interface (use-case-shaped, not CRUD-shaped)
- Seed generator (writes to either backend via `Store`)
- Contract tests (run against both backends via testcontainers)
- Perf harness (HTTP-level, backend-agnostic)
- docker-compose

### Per-DB code (write twice)

| Concern                  | MongoDB                 | PostgreSQL                                       |
| ------------------------ | ----------------------- | ------------------------------------------------ |
| Schema                   | Collections + documents | Tables + migrations (`goose` / `golang-migrate`) |
| Full-text / fuzzy search | Atlas Search            | `pg_trgm` + `tsvector`                           |
| Vector search            | Atlas Vector Search     | `pgvector` (HNSW index)                          |
| Change events            | Change Streams          | `LISTEN/NOTIFY`                                  |
| Aggregations             | Aggregation pipeline    | SQL / CTE                                        |
| Indexes                  | Compound, text, vector  | B-tree, GIN, HNSW                                |

### Interface discipline

`Store` is use-case-shaped so each impl can be idiomatic:

- `SearchPayments(ctx, query, filters)` — fuzzy + filter
- `FindSimilarPayments(ctx, vec, k, filters)` — vector kNN
- `GetPaymentTimeline(ctx, customerID, window)` — for fraud context
- `SubscribeToPaymentEvents(ctx)` — returns a channel

No `Find(collection, filter)` or `Query(sql, args)`. Interface hides _how_.

## Build phasing (shape only — day-by-day is a separate doc)

| Phase         | Rough days | Output                                                                                                                                                            |
| ------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Skeleton      | 1–2        | Repo, docker-compose, Next.js shell, `Store` interface, one thin slice (create + list payment) against Mongo.                                                     |
| Mongo build   | 3–6        | Control centre UI, search, similar-case retrieval (Voyage + Vector Search), live event feed (Change Streams), fraud worker (agentic model). Contract tests green. |
| Postgres port | 7–10       | Schema + migrations, PG `Store` impl, `pg_trgm` + `tsvector`, `pgvector`, `LISTEN/NOTIFY`. Same contract tests green against PG.                                  |
| Perf          | 11–13      | k6 (or chosen tool) scenarios covering the key user flow chains. Run against both. Tune indexes/queries on M50-equivalent. Comparison report.                     |
| Buffer + demo | 14         | README, demo script, TFW talking points.                                                                                                                          |

## Verification (how we'll know it's done)

Same demo script runs identically against both backends:

1. `make seed BACKEND=mongo|postgres` populates the chosen store.
2. Support worker opens control centre → fuzzy-searches partial customer ref → gets ranked matches.
3. Opens a failed payment → "similar cases" panel shows vector-nearest historical failures.
4. Fraud panel shows the agentic model's decision + reasoning for the payment.
5. Chat: "why did this fail?" — agent retrieves context via Vector Search / `pgvector` and answers.
6. Live event feed shows a newly-landing payment without page refresh (Change Streams / `LISTEN/NOTIFY`).
7. Perf run: HTTP load tool exercises the key chain (search → detail → similar → chat), reports p50/p95/p99, error rate, throughput for both DBs on the same scenario.

If all 7 steps pass on both backends and the perf report is legible, v1 is done.

## Deliverables from this challenge

1. GitHub repo (single) with both `Store` implementations and both docker-compose profiles.
2. README with run instructions, feature callouts for TFW, and sample walkthrough scenarios.
3. Short performance report comparing the two backends on the same scenarios, with the KPIs and graphs that matter for a customer conversation.
4. Note on "was one DB easier to build with Claude?" — captured while building, not retrofitted.
