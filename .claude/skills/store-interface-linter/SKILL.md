---
name: store-interface-linter
description: Rejects CRUD-shaped methods on the Store interface. Enforces the "Store is use-case-shaped, not CRUD-shaped" rule from CLAUDE.md. Trigger whenever editing files under store/ or the Store interface definition, or when the user says "add Store method" / "add a Query method" / "generic find".
---

# Store interface linter

The `Store` interface hides *how* — each backend can be idiomatic. CRUD-shaped methods leak the how and force both impls to converge on a lowest-common-denominator.

## Forbidden shapes

- `Find(collection string, filter map[string]any)` — Mongo-flavoured leakage.
- `Query(sql string, args ...any)` — SQL-flavoured leakage.
- `Get(id string)` on its own without a use case — usually a smell; ask "get for what?".
- Generic pagination cursors that expose driver internals.
- Method names ending in `ByX` where X is a column — that's a CRUD tell.

## Required shape

Method names describe a **user-visible action or a domain question**, not a storage operation. From `docs/plan.md`:

- `SearchPayments(ctx, query, filters)` — fuzzy + filter
- `FindSimilarPayments(ctx, vec, k, filters)` — vector kNN
- `GetPaymentTimeline(ctx, customerID, window)` — for fraud context
- `SubscribeToPaymentEvents(ctx)` — returns a channel

The Mongo impl uses `$search` + `$vectorSearch` + Change Streams. The PG impl uses `pg_trgm` + `pgvector` + `LISTEN/NOTIFY`. Neither shows in the signature.

## What to do when invoked

1. Read the current `Store` interface definition (usually `internal/store/store.go` or `store/store.go`).
2. For each method, check the name and signature against the rules above.
3. Report violations one line each. Suggest a use-case-shaped alternative.
4. If asked to add a new method: ask "what use case does this serve?" first. Refuse to add it if the answer is "any query with these fields".

## Output

```
✗ Find(coll, filter map[string]any)        — leaks Mongo. Use: SearchPayments / GetPaymentTimeline.
✗ QueryPayments(sql string, args ...any)   — leaks SQL. Use: SearchPayments.
✓ FindSimilarPayments(ctx, vec, k)         — use-case-shaped.
```

Nothing else.
