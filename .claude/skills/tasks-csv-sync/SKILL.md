---
name: tasks-csv-sync
description: Updates the embedded task list in tasks.html when a feature ships or scope changes. Marks rows done, adds rows for unlisted work, flags stale rows. Trigger when the user says "sync tasks", "update tasks", "mark this done", or after a commit that touched non-trivial code. (Named for legacy reasons — the data lives in tasks.html, not a CSV.)
---

# tasks sync

`tasks.html` (the `<script id="tasks-data" type="application/json">` block inside it) is the single source of truth for progress. Every merged change should be reflected there.

## Inputs

- Latest commit diff (or working diff if no commit yet).
- Current `tasks.html` — specifically the `tasks-data` JSON block.

## What to do

1. Read the diff. Group changes by area — API, `store/mongo`, `store/postgres`, frontend, worker, tests, docs.
2. For each area, find the matching row(s) in the JSON array.
3. Actions, in order of preference:
   - **Mark `"status": "done"`** if the diff completes the row's scope.
   - **Mark `"status": "doing"`** if it's partial.
   - **Append a new row** if the change doesn't map to any existing task (real scope creep — flag it).
   - **Split a row** only if the user asks.
4. **Always update `paths`** on any row you touch. Value is a comma-separated list of files or dirs where the logic lives (e.g. `"store/mongo/payments.go, internal/domain/payment.go"`). If the change spans a directory, name the dir with a trailing `/`. Keep it short — the reader wants a jump list, not a manifest.
5. Show the proposed diff of the JSON block. Wait for confirmation before writing.

## Rules

- Never invent phases outside `docs/plan.md`.
- Never bulk-mark rows done — one commit rarely finishes multiple tasks.
- If a diff touches `store/**` but the matching contract test wasn't updated, do NOT mark the row done — flag it: "contract test missing, cannot mark done".
- Never modify `id` values of existing rows. Append only.
- `status` values are exactly `"todo"`, `"doing"`, `"done"`. No other values.
- Never mark a row `doing` or `done` without a `paths` value. If you can't locate the code, the row isn't ready to move.
- If the diff adds/removes a container, external service, queue, or DB — update `architecture.md` **and** `architecture.html` in the same change. They're a matched pair.
- Preserve JSON formatting/alignment of surrounding rows to keep diffs small.
- Do not touch any part of `tasks.html` outside the `tasks-data` script block.

## Output

```
Proposed changes to tasks.html (tasks-data):

  ~ #11  "status": "doing" → "done", "paths": "" → "store/mongo/search.go, store/mongo/indexes/atlas_search.json"
  ~ #12  "status": "todo"  → "doing", "paths": "" → "internal/embed/voyage.go"
  + #69  new row: "Refund flow (unlisted — was in commit abc123)", paths: "internal/api/refunds.go"

Confirm? [y/n]
```
