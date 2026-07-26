---
name: usecase-to-tasks
description: Turns a use case entry from usecases.md into rows in tasks.csv (rendered by tasks.html). Trigger when the user says "convert usecase X to tasks", "break down this usecase", or invokes /usecase-to-tasks.
---

# usecase → tasks

Every use case must land in `tasks.csv` before implementation starts (CLAUDE.md rule).

## Inputs

- Usecase ID or heading from `usecases.md`.
- If none given, ask which one.

## What to do

1. Read the named section of `usecases.md`.
2. Read the existing `tasks.csv` header — do not invent columns. Current columns:
   `id, phase, title, backend, status`
3. Break the use case into 3–8 rows. Fewer is better. Each row is one shippable slice.
4. Assign:
   - `phase` — one of the phases in `docs/plan.md` (`Skeleton`, `Mongo build`, `Postgres port`, `Perf`, `Buffer + demo`). Never invent new phases.
   - `backend` — `mongo`, `postgres`, `both`, or `common` (frontend/API/DTO).
   - `status` — always `todo` for new rows.
   - `id` — next integer after the current max.
5. Append rows to `tasks.csv`. Do not rewrite existing rows.
6. Show the diff to the user before saving.

## Rules

- One task per shippable slice. Not per file, not per function.
- If a task only makes sense for one backend, don't create the mirror on the other side — the contract test suite handles parity.
- If you can't map a use case to the existing phases, stop and tell the user — the plan needs updating, not the tasks file.
- Never edit `tasks.html` — it reads the CSV.

## Output

```
Appended 4 rows to tasks.csv:

36, Mongo build,     Payment refund flow: refund record + reversal event,  mongo,     todo
37, Mongo build,     Refund UI: refund button + confirmation modal,        common,    todo
38, Postgres port,   Payment refund flow (PG impl),                        postgres,  todo
39, Buffer + demo,   Add refund step to demo script,                       common,    todo
```

Then stop.
