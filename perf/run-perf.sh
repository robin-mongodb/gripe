#!/usr/bin/env bash
# Runs one k6 scenario pass and immediately persists the full summary to Atlas
# (database `gripe_perf`, collection `results`) so results survive the EC2
# cleanup reaper. The insert happens AFTER the run ends so it can't skew the
# numbers; gripe_perf is separate from the benchmarked `gripe` database.
#
# Usage: ./run-perf.sh <postgres|mongo> [RATE] [DURATION]
#   BASE defaults to the app box (see /etc/motd); override with BASE=... env.
#   MONGO_URI comes from perf/.env (written by loadgen user-data) or the env.
set -euo pipefail
cd "$(dirname "$0")"

LABEL="${1:?usage: run-perf.sh <postgres|mongo> [RATE] [DURATION]}"
RATE="${2:-1}"
DURATION="${3:-2m}"
[ -f .env ] && . ./.env
: "${MONGO_URI:?MONGO_URI not set (perf/.env or env)}"
: "${BASE:?BASE not set, e.g. BASE=http://<app-private-ip>:8080/v1}"

# k6 exits non-zero when thresholds are crossed — that's data, not a failure;
# persist the summary regardless.
k6 run -e BASE="$BASE" -e MERCHANTS=50 -e RATE="$RATE" -e DURATION="$DURATION" \
  -e LABEL="$LABEL" scenarios.js || echo "k6 exit $? (thresholds crossed) — persisting anyway"

GIT_SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
FILE="results-$LABEL.json"

# fs is available in mongosh's Node environment; metadata makes runs queryable.
mongosh "$MONGO_URI" --quiet --eval "
  const doc = {
    label: '$LABEL',
    rate: Number('$RATE'),
    duration: '$DURATION',
    git_sha: '$GIT_SHA',
    run_at: new Date(),
    summary: JSON.parse(fs.readFileSync('$FILE', 'utf8')),
  };
  const res = db.getSiblingDB('gripe_perf').results.insertOne(doc);
  print('persisted to gripe_perf.results: ' + res.insertedId);
"
