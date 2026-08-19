# Was one database easier to build with Claude? (task 47)

Short answer: **neither was meaningfully easier to *build*; they were easier in
different places.** The thing that actually made the two-backend approach work
was the shared contract suite, not either database.

## Where PostgreSQL was easier

- **The schema is a spec.** FKs, CHECK constraints, and NOT NULL turned
  generated-code mistakes into immediate, loud errors. When Claude got a column
  or a state transition wrong, the database said so on the first test run. With
  Mongo, a wrong field name just writes a new field — two of the remodel bugs
  only surfaced because a contract test asserted on the value.
- **SQL is abundant training data.** First-pass CTEs, `UPDATE ... FROM ...
  RETURNING`, and keyset pagination came out correct. There was essentially no
  "look up the operator" loop.
- **Transactions are one idiom.** Begin/defer-rollback/commit is the same shape
  for every multi-step write; no per-case design discussion was needed.

## Where MongoDB was easier

- **Schema change is a non-event.** The idiomatic remodel (embedding refunds,
  balance maps) shipped on the Mongo side as *code only* — drop the database,
  `ensureIndexes` on boot, done. The PG side needed a new goose migration with
  drop/recreate DDL and the discipline of never touching applied migrations
  (00001–00003 were already on RDS).
- **Single-document atomicity replaced transaction plumbing.** The refund path
  (guard + increment + append + status flip) became one aggregation-pipeline
  `FindOneAndUpdate` — no tx, no crash window, and it also sidestepped the fact
  that testcontainers runs a standalone `mongod` (no transactions available).
- **Document shape matches the API shape.** Mapping structs to docs was nearly
  mechanical; the PG side needed more mapping code (rows ↔ nested types).

## Where each one bit us

- **Mongo:** performance is sensitive to details a first pass gets wrong.
  Claude's original list query sorted by `(created_at, _id)` against an index on
  `(merchant_id, created_at)` — silently correct, silently slow (in-memory sort
  per call). It took the perf phase to find it. Also: a unique index on an
  embedded-array field breaks on empty arrays; that nuance needed a retry.
- **PostgreSQL:** operational sharp edges, not code ones. The RDS Multi-AZ
  cluster fought Terraform (gp3 IOPS rules, a provider bug needing
  `ignore_changes`), and `pgxpool` defaults to ~4 connections on a small box —
  invisible until load testing.

## The real lesson

The **contract suite run against both backends via testcontainers** was the
multiplier. Claude could rewrite either store aggressively (the task-42 tuning,
the full remodel) and a green suite meant behavior parity — 47 subtests catching
what type systems and databases individually could not. Building the same
product twice cost far less than 2× because the tests, API, seed, and perf
harness were written once.

If forced to pick: for *getting to correct* fastest, PostgreSQL's constraints
gave Claude a tighter feedback loop. For *iterating on a design*, MongoDB's
flexibility made the second and third attempts cheaper. Over the whole project
that netted out to a draw — and the perf numbers ended up saying the same thing
(see `docs/perf-report.html`).
