# Perf harness

Load tool: **k6** (task 37 decision, 2026-08-06).

Why k6 over locust/vegeta: scenario scripting in JS (create → capture → refund flows
need request chaining, not just constant-rate GETs), built-in p50/p95/p99 summary
output, single static binary on the demo box, and thresholds for pass/fail gating.

Scenarios (task 38) live in this directory as `*.js`. Run against a backend with:

    k6 run -e BASE=http://<host>/v1 scenarios.js

Same scripts run against both backends — only `GRIPE_BACKEND` on the API changes.
