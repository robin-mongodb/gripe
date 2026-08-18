# Perf harness

Load tool: **k6** (task 37 decision, 2026-08-06).

Why k6 over locust/vegeta: scenario scripting in JS (create → capture → refund flows
need request chaining, not just constant-rate GETs), built-in p50/p95/p99 summary
output, single static binary on the demo box, and thresholds for pass/fail gating.

Scenarios (task 38) live in this directory as `*.js`. Run against a backend with:

    k6 run -e BASE=http://<host>/v1 scenarios.js

Same scripts run against both backends — only `GRIPE_BACKEND` on the API changes.

## Seed volume (task 39 decision, 2026-08-06)

**50 merchants × 2,000 payments (100k payments), 20 customers/merchant, 10 subscriptions/merchant (500 subs).**

    docker compose run --rm seed -m 50 -p 2000 -c 20 -s 10

Rationale: big enough that the merchant-scoped list indexes matter (2k payments/merchant ≫ one
page) and the volume/balance aggregates do real work; small enough to seed in minutes on the
demo box. ~60% of settleable payments get settled by the seeder, so balances are non-trivial.
Revisit with the seed-tuner agent if p95s look index-insensitive at this size.

## Running the comparison (tasks 40/41)

On the loadgen box (`terraform output loadgen_public_ip`; app box private IP from `terraform output`):

    # 1. API on mongo (GRIPE_BACKEND=mongo in /opt/gripe/.env on the app box), seeded
    k6 run -e BASE=http://<app-private-ip>:8080/v1 -e MERCHANTS=50 -e LABEL=mongo scenarios.js

    # 2. Flip .env to GRIPE_BACKEND=postgres on the app box, `docker compose up -d`, re-seed, then
    k6 run -e BASE=http://<app-private-ip>:8080/v1 -e MERCHANTS=50 -e LABEL=postgres scenarios.js

Each run writes `results-<label>.json` for the comparison report (task 43).

Prefer `./run-perf.sh <postgres|mongo> [RATE] [DURATION]` over raw `k6 run` — it runs the
same scenarios, then immediately persists the full summary (plus label/rate/duration/git
sha/timestamp) to Atlas `gripe_perf.results` via mongosh + MONGODB-AWS, so results survive
the EC2 cleanup reaper. Separate database from the benchmarked `gripe`; insert happens
after the run ends so it can't skew latencies. Query back with:

    mongosh "$MONGO_URI" --eval 'db.getSiblingDB("gripe_perf").results.find({}, {label:1, rate:1, run_at:1}).sort({run_at:-1})'
