---
name: usecase-to-tasks
description: Turns a use case entry from usecases.md into rows in the tasks data embedded in tasks.html. Trigger when the user says "convert usecase X to tasks", "break down this usecase", or invokes /usecase-to-tasks.
---

# usecase → tasks

Every use case must land in `tasks.html` (in the `<script id="tasks-data">` block) before implementation starts (CLAUDE.md rule).

## Inputs

- Usecase ID or heading from `usecases.md`.
- If none given, ask which one.

## What to do

1. Read the named section of `usecases.md`.
2. Read `tasks.html` and find the `<script id="tasks-data" type="application/json">` block. That JSON array is the single source of truth. Column keys are:
   `id, usecase, phase, title, backend, status, paths`
3. Break the use case into 3–8 rows. Fewer is better. Each row is one shippable slice.
4. Assign:
   - `usecase` — the exact ID (e.g. `"UC-4"`).
   - `phase` — one of the phases in `docs/plan.md` (`Skeleton`, `Mongo build`, `Postgres port`, `Customer checkout`, `Perf`, `Buffer + demo`). Never invent new phases.
   - `backend` — `mongo`, `postgres`, `both`, or `common`.
   - `status` — always `"todo"` for new rows.
   - `paths` — always `""` for new rows. It's filled in by `tasks-csv-sync` when work starts.
   - `id` — string, next integer after the current max.
5. Append the new objects to the JSON array in `tasks.html`. Do not rewrite existing rows.
6. Show the diff to the user before saving.

## Rules

- One task per shippable slice. Not per file, not per function.
- If a task only makes sense for one backend, don't create the mirror on the other side — the contract test suite handles parity.
- If you can't map a use case to the existing phases, stop and tell the user — the plan needs updating, not the tasks data.
- Never edit any part of `tasks.html` outside the `tasks-data` script block.
- Preserve the alignment/whitespace style of the existing rows so diffs stay readable.

## Output

```
Appended 4 rows to tasks.html <script id="tasks-data">:

  { "id": "69", "usecase": "UC-4", "phase": "Mongo build", "title": "…", "backend": "mongo",    "status": "todo" }
  { "id": "70", "usecase": "UC-4", "phase": "Mongo build", "title": "…", "backend": "common",   "status": "todo" }
  ...
```

Then stop.
