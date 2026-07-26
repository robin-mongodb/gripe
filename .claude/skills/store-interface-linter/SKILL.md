---
name: store-interface-linter
description: Rejects CRUD-shaped methods on the Store interface. Enforces the "Store is use-case-shaped, not CRUD-shaped" rule from CLAUDE.md. Trigger whenever editing files under store/ or the Store interface definition, or when the user says "add Store method" / "add a Query method" / "generic find".
---

# Store interface linter

The `Store` interface hides *how* — each backend can be idiomatic. CRUD-shaped methods leak the how and force both impls to converge on a lowest-common-denominator.

## Forbidden shapes

- `Find(collection string, filter map[string]any)` — Mongo-flavoured leakage.
- `Query(sql string, args ...any)` — SQL-flavoured leakage.
- `Get(id string)` on its own without an actor scope — usually a smell; a merchant reading a payment is a different use case from an admin reading one.
- Generic pagination cursors that expose driver internals.
- Method names ending in `ByX` where X is a column — that's a CRUD tell.

## Required shape

Method names describe a **user-visible action or a domain question**, not a storage operation. From `docs/plan.md`:

- `CreatePayment(ctx, input, idempotencyKey)`
- `CapturePayment(ctx, paymentID)`
- `SettlePayment(ctx, paymentID)` — includes the merchant-balance credit as one atomic op
- `RefundPayment(ctx, paymentID, amount, idempotencyKey)`
- `SettleRefund(ctx, refundID)` — includes the merchant-balance debit as one atomic op
- `GetPayment(ctx, id, actor)` — actor scoping is part of the use case, not a wrapper concern
- `ListMerchantPayments(ctx, merchantID, filters, cursor)`
- `ListAllPayments(ctx, filters, cursor)` — admin
- `GetMerchantBalances(ctx, merchantID)` — per-currency map; no single-currency variant
- `CreateSubscription(ctx, input)`
- `CancelSubscription(ctx, subscriptionID)`
- `DueSubscriptions(ctx, asOf, limit)`

Idempotency is a parameter on the write, not a separate `PutIdempotencyKey` method — the store hides how it's persisted.

**No `AdjustBalance` / `CreditBalance` / `DebitBalance` methods.** Balance moves are a side effect of `SettlePayment` and `SettleRefund`. A CRUD-shaped balance method would let handlers drift out of sync with settled amounts — reject it on sight.

## What to do when invoked

1. Read the current `Store` interface definition (usually `internal/store/store.go` or `store/store.go`).
2. For each method, check the name and signature against the rules above.
3. Report violations one line each. Suggest a use-case-shaped alternative.
4. If asked to add a new method: ask "what use case does this serve? which persona?" first. Refuse to add it if the answer is "any query with these fields".

## Output

```
✗ Find(coll, filter map[string]any)          — leaks Mongo. Use one of the list methods.
✗ QueryPayments(sql string, args ...any)     — leaks SQL. Use ListMerchantPayments / ListAllPayments.
✗ GetPaymentByMerchant(id, merchantID)       — CRUD tell; roll actor scoping into GetPayment(ctx, id, actor).
✓ RefundPayment(ctx, paymentID, amount, key) — use-case-shaped.
```

Nothing else.
