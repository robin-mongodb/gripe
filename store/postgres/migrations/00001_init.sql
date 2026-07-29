-- +goose Up
-- +goose StatementBegin

-- Amounts are stored as bigint minor-units (matches the domain DTO's int64 AmountMinor).
-- docs/plan.md § Money originally said numeric(19,4); we picked minor-units in Go end-to-end.
-- Both are precise; bigint is one atomic type across driver + Mongo mirror.

CREATE TABLE IF NOT EXISTS payments (
    id              TEXT PRIMARY KEY,
    merchant_id     TEXT NOT NULL,
    customer_id     TEXT NOT NULL,
    amount_minor    BIGINT NOT NULL CHECK (amount_minor > 0),
    currency        TEXT NOT NULL CHECK (currency IN ('USD','GBP','EUR')),
    method          TEXT NOT NULL CHECK (method IN ('card','direct_debit','bank_transfer','apple_pay','google_pay')),
    status          TEXT NOT NULL CHECK (status IN ('authorized','captured','settled','pending','declined','refunded')),
    refunded_minor  BIGINT NOT NULL DEFAULT 0 CHECK (refunded_minor >= 0 AND refunded_minor <= amount_minor),
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

-- Task 30: composite for merchant-scoped listing.
-- Equality (merchant_id) -> sort (created_at DESC). Same shape as Mongo.
CREATE INDEX IF NOT EXISTS payments_merchant_created_idx
    ON payments (merchant_id, created_at DESC);

-- Admin cross-merchant listing hits (created_at DESC) alone; small enough table
-- for a demo that we skip a dedicated index for now.

CREATE TABLE IF NOT EXISTS refunds (
    id            TEXT PRIMARY KEY,
    payment_id    TEXT NOT NULL REFERENCES payments(id),
    amount_minor  BIGINT NOT NULL CHECK (amount_minor > 0),
    currency      TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS refunds_payment_created_idx
    ON refunds (payment_id, created_at DESC);

CREATE TABLE IF NOT EXISTS subscriptions (
    id                TEXT PRIMARY KEY,
    merchant_id       TEXT NOT NULL,
    customer_id       TEXT NOT NULL,
    amount_minor      BIGINT NOT NULL CHECK (amount_minor > 0),
    currency          TEXT NOT NULL CHECK (currency IN ('USD','GBP','EUR')),
    method            TEXT NOT NULL,
    cadence           TEXT NOT NULL CHECK (cadence IN ('daily','weekly','monthly')),
    status            TEXT NOT NULL CHECK (status IN ('active','cancelled')),
    next_charge_at    TIMESTAMPTZ NOT NULL,
    next_cycle_index  BIGINT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ
);

-- Cycler query: {status = 'active' AND next_charge_at <= now()}.
CREATE INDEX IF NOT EXISTS subs_status_next_charge_idx
    ON subscriptions (status, next_charge_at);

-- Task 29: idempotency store. Unique on key; lazy expiry via a plain column checked in code
-- (matches Mongo's TTL index semantics without needing pg_cron).
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key         TEXT PRIMARY KEY,
    actor_role  TEXT NOT NULL,
    actor_id    TEXT NOT NULL,
    body_hash   TEXT NOT NULL,
    status      INTEGER NOT NULL,
    response    BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idem_expires_idx ON idempotency_keys (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS refunds;
DROP TABLE IF EXISTS payments;
-- +goose StatementEnd
