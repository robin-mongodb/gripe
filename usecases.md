# Gripe — Use cases

Scope: a **payments processing platform** built on both MongoDB and PostgreSQL (RDS PostgreSQL in prod). Three personas — Gripe employee (admin), merchant, customer — on top of the same backend. Each use case must work identically against both backends (contract tested) and map to rows in `tasks.html`.

Fraud detection, AI chat, and vector search are **parked** — see CLAUDE.md and `docs/plan.md`.

---

## UC-1: Create a payment (any method)

**Persona:** merchant (via API) or customer (via checkout, later phase)
**So that:** money can move.

- Backend endpoint: `POST /v1/payments` with `Idempotency-Key` header.
- Supported methods, all mocked: `card`, `direct_debit`, `bank_transfer`, `apple_pay`, `google_pay`.
- Card / Apple Pay / Google Pay: auth + capture synchronous (mock succeeds unless the amount ends in `.13` → simulated decline).
- Direct debit: auth-only, settles after N cycler ticks.
- Bank transfer: pending until an async "settled" event lands.
- Store method: `CreatePayment(ctx, input, idempotencyKey)`.

**Done when:** all five methods produce the right terminal state on both backends; the mock decline path is covered by a contract test.

## UC-2: Idempotent payment creation

**Persona:** merchant (any integrator that retries).
**So that:** network retries don't create duplicate charges.

- Same `Idempotency-Key` + same body → identical response, no new payment row.
- Same key + different body → 409, name the conflict.
- Keys expire after 24h (both backends).

**Done when:** contract test covers same-body-replay, different-body-conflict, and post-expiry replay on both backends.

## UC-3: Capture a previously authorized payment

**Persona:** merchant.
**So that:** merchants who authorize now and ship later can capture on their own schedule.

- `POST /v1/payments/{id}/capture`.
- Only valid on authorized-but-uncaptured card payments.
- Store method: `CapturePayment(ctx, paymentID)`.

**Done when:** capture succeeds on an authorized payment and fails cleanly on any other state, both backends.

## UC-4: Refund a payment (merchant-chosen amount)

**Persona:** merchant (self-serve), Gripe employee (support override).
**So that:** money moves back when needed, on the merchant's terms.

- `POST /v1/payments/{id}/refunds` with `{amount, idempotency_key}`.
- The merchant chooses the amount. Constraint: `0 < amount ≤ (captured_amount − already_refunded)`.
- Over-refund → 422 with the remaining refundable amount in the response body.
- Sum of refunds ≤ captured amount — enforced in the store, not the handler.
- Direct debit refunds queue for the cycler; card refunds settle synchronously.
- Store method: `RefundPayment(ctx, paymentID, amount, idempotencyKey)`.
- Side effect: debits the merchant balance by `amount − (amount × 0.03)` (mirror the credit rule in UC-11).

**Done when:** partial refunds, full refunds, over-refund rejection, and the balance debit all pass the contract suite on both backends.

## UC-5: Merchant sees only their own payments

**Persona:** merchant.
**So that:** a merchant can operate their dashboard without seeing anyone else's data.

- `GET /v1/payments` filtered by `X-Actor-Id` when `X-Actor-Role: merchant`.
- Filters: status, method, date range, amount range.
- Cursor pagination.
- Store method: `ListMerchantPayments(ctx, merchantID, filters, cursor)`.

**Done when:** merchant A cannot read merchant B's payment by direct ID (`GetPayment` denies), both backends.

## UC-6: Gripe employee sees every merchant and every payment

**Persona:** Gripe employee (admin).
**So that:** support and ops can trace anything.

- Same list endpoint but no merchant filter when `X-Actor-Role: admin`.
- Additional filter: `merchant_id`.
- Aggregate view: payment volume per merchant per day.
- Store methods: `ListAllPayments(ctx, filters, cursor)` + a dashboard aggregate method.

**Done when:** admin console loads a 30-day cross-merchant summary under 300ms p95 on seeded data, both backends.

## UC-7: Create a subscription

**Persona:** merchant.
**So that:** merchants can bill on a schedule without writing a scheduler.

- `POST /v1/subscriptions` with amount, cadence (`daily|weekly|monthly`), payment method, customer.
- Store method: `CreateSubscription(ctx, input)`.

**Done when:** subscription persists with a correct `next_charge_at`, both backends.

## UC-8: Subscription cycler creates the next payment on schedule

**Persona:** system (background worker).
**So that:** subscriptions actually charge without a human trigger.

- Worker polls `DueSubscriptions(ctx, asOf, limit)` on a short interval.
- For each due subscription: create a payment via the same `CreatePayment` path (same idempotency), advance `next_charge_at`.
- Idempotent per (subscription_id, cycle_index) — cycler crashes must not double-charge.

**Done when:** running the cycler twice back-to-back produces exactly one payment per due subscription per cycle, both backends.

## UC-9: Cancel a subscription

**Persona:** merchant or customer.
**So that:** recurring charges stop.

- `POST /v1/subscriptions/{id}/cancel`.
- Merchant can cancel any of their own; customer can cancel any of theirs.
- Store method: `CancelSubscription(ctx, subscriptionID)`.

**Done when:** cancelled subscription is skipped by the cycler on the next tick, both backends.

## UC-11: Merchant balance credited on payment (minus Gripe fee)

**Persona:** system (side effect of payment settlement).
**So that:** merchants can see what they've actually earned, net of Gripe's cut.

- Every merchant has a `balance` **per currency** — see UC-12. Payments in USD only touch the USD balance; same for GBP and EUR.
- On payment settlement (card/Apple Pay/Google Pay: synchronous; direct debit / bank transfer: on the async settle event), credit the merchant balance by `amount − fee`, where `fee = round(amount × 0.03, 2)` (currency's smallest-unit rounding).
- On refund settlement (UC-4), debit by `refund_amount − refund_fee` where `refund_fee = round(refund_amount × 0.03, 2)`. Fees are not returned to the merchant on refund — they're clawed back from the merchant's balance, matching Stripe's behaviour.
- Atomic with the payment/refund state transition — either both happen or neither. Contract test asserts that a mid-write crash never leaves balance out of sync with settled amounts.
- Fee rate `0.03` lives in one config constant; a contract test pins it.
- Store methods: `SettlePayment(ctx, paymentID)` and `SettleRefund(ctx, refundID)` update balance as part of the same operation. No separate `AdjustBalance` — that would be CRUD-shaped and let handlers drift out of sync.

**Done when:** settling N payments totalling `X` leaves the merchant's balance at exactly `X − round(sum(amount × 0.03))`, and refunding brings it down by `refund_net`, on both backends. No path exists to move the balance without a corresponding payment or refund.

## UC-12: Multi-currency (USD / GBP / EUR)

**Persona:** merchant, customer.
**So that:** merchants can accept payments in any of the three supported currencies without FX.

- Every payment carries `currency ∈ {USD, GBP, EUR}`. Reject anything else at the API boundary.
- Merchant balance is **per currency** — a merchant has one balance per currency they've received (`balances: {USD: ..., GBP: ..., EUR: ...}`).
- Refund currency must equal the original payment currency; server enforces.
- Fee is 3% of the payment amount in the payment's currency — no conversion.
- Fee ledger entries are also per-currency (Gripe accumulates USD/GBP/EUR fee revenue separately).
- Rounding: to the currency's smallest unit (2 dp for all three). Round half-even.
- Merchant dashboard shows one balance row per currency the merchant has activity in.

**Done when:** creating payments in all three currencies for one merchant produces three independent balances that never mix, and refunding a USD payment only touches the USD balance — on both backends.

## UC-10: Customer checkout (last)

**Persona:** paying customer.
**So that:** a customer can complete a payment on a merchant's Gripe-hosted checkout page.

- Next.js checkout surface for a merchant.
- Picks a payment method; hits `POST /v1/payments` with customer + merchant context.
- Success page + failure page.

**Done when:** each of the five mock methods completes end-to-end for a customer, on both backends. This is intentionally the last thing built.

---

## Out of scope (deliberate)

- **Auth.** Actor identity is a header.
- **Real PSP integration.** All methods are mocked.
- **Fraud detection / AI chat / vector search / fuzzy search / live event feed.** Parked; may return in a later phase.
- **Currency conversion / FX.** Payments carry a currency code (USD/GBP/EUR only — see UC-12). Conversion between them is out of scope.
- **Currencies beyond USD/GBP/EUR.** Adding a fourth is a schema decision — flag it if requested.
- **Disputes / chargebacks.** Not in v1.
- **Payouts to merchant bank accounts.** Not in v1.
- **Mobile / native apps.**
