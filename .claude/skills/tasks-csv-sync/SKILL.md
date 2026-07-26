---
name: tasks-csv-sync
description: Updates tasks.csv when a feature ships or scope changes. Marks rows done, adds rows for unlisted work, flags stale rows. Trigger when the user says "sync tasks", "update tasks.csv", "mark this done", or after a commit that touched non-trivial code.
---

# tasks.csv sync

`tasks.csv` is the single source of truth for progress. Every merged change should be reflected there.

## Inputs

- Latest commit diff (or working diff if no commit yet).
- Current `tasks.csv`.

## What to do

1. Read the diff. Group changes by area — API, `store/mongo`, `store/postgres`, frontend, worker, tests, docs.
2. For each area, find the matching row(s) in `tasks.csv`.
3. Actions, in order of preference:
   - **Mark `done`** if the diff completes the row's scope.
   - **Mark `doing`** if it's partial.
   - **Append a new row** if the change doesn't map to any existing task (real scope creep — flag it).
   - **Split a row** only if the user asks.
4. Show the proposed CSV diff. Wait for confirmation before writing.

## Rules

- Never invent phases outside `docs/plan.md`.
- Never bulk-mark rows done — one commit rarely finishes multiple tasks.
- If a diff touches `store/**` but the matching contract test wasn't updated, do NOT mark the row done — flag it: "contract test missing, cannot mark done".
- Never modify `id` values of existing rows. Append only.
- `status` values are exactly `todo`, `doing`, `done`. No other values.

## Output

```
Proposed changes to tasks.csv:

  ~ 11  Mongo build   Atlas Search fuzzy search implementation           mongo    doing → done
  ~ 12  Mongo build   Voyage embeddings pipeline for payments            common   todo → doing
  + 36  Mongo build   Refund flow (unlisted — was in commit abc123)      mongo    todo

Confirm? [y/n]
```
