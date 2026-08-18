-- +goose Up
-- +goose StatementBegin

-- Remodel (tasks 73-75): merchants and customers become first-class tables with
-- full FK integrity, and refunds are fully normalized (merchant/currency derive
-- from the parent payment). This is a drop-and-recreate: demo data is wiped by
-- design — there is no production data worth migrating, and rebuilding from the
-- clean shape is simpler and safer than in-place column surgery.
-- ON DELETE RESTRICT everywhere: these are financial records; deletes should be loud.

DROP TABLE IF EXISTS merchant_balances;
DROP TABLE IF EXISTS refunds;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS payments;
-- idempotency_keys is left alone: it references nothing and its keys stay valid.

CREATE TABLE merchants (
    id         TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE customers (
    id         TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Same column/CHECK shape as 00001+00003, plus FKs to the new parent tables.
CREATE TABLE payments (
    id                 TEXT PRIMARY KEY,
    merchant_id        TEXT NOT NULL REFERENCES merchants(id) ON DELETE RESTRICT,
    customer_id        TEXT NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    amount_minor       BIGINT NOT NULL CHECK (amount_minor > 0),
    currency           TEXT NOT NULL CHECK (currency IN ('USD','GBP','EUR')),
    method             TEXT NOT NULL CHECK (method IN ('card','direct_debit','bank_transfer','apple_pay','google_pay')),
    status             TEXT NOT NULL CHECK (status IN ('authorized','captured','settled','pending','declined','refunded')),
    refunded_minor     BIGINT NOT NULL DEFAULT 0 CHECK (refunded_minor >= 0 AND refunded_minor <= amount_minor),
    network_fee_minor  BIGINT, -- NULL = fee-worker hasn't set it yet (set exactly once)
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL
);

-- Merchant-scoped keyset listing: equality (merchant_id) -> sort (created_at DESC, id DESC).
CREATE INDEX payments_merchant_created_id_idx
    ON payments (merchant_id, created_at DESC, id DESC);

-- FK-side lookup; also keeps customer deletes from seq-scanning payments.
CREATE INDEX payments_customer_idx ON payments (customer_id);

-- Admin cross-merchant keyset listing.
CREATE INDEX payments_created_id_idx ON payments (created_at DESC, id DESC);

-- Fully normalized: merchant_id and currency derive from the parent payment.
CREATE TABLE refunds (
    id            TEXT PRIMARY KEY,
    payment_id    TEXT NOT NULL REFERENCES payments(id) ON DELETE RESTRICT,
    amount_minor  BIGINT NOT NULL CHECK (amount_minor > 0),
    status        TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','settled')),
    created_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX refunds_payment_idx ON refunds (payment_id);

CREATE TABLE subscriptions (
    id                TEXT PRIMARY KEY,
    merchant_id       TEXT NOT NULL REFERENCES merchants(id) ON DELETE RESTRICT,
    customer_id       TEXT NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
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
CREATE INDEX subs_status_next_charge_idx
    ON subscriptions (status, next_charge_at);

-- Per-merchant per-currency ledger. PK doubles as the unique (merchant_id, currency).
CREATE TABLE merchant_balances (
    merchant_id   TEXT   NOT NULL REFERENCES merchants(id) ON DELETE RESTRICT,
    currency      TEXT   NOT NULL CHECK (currency IN ('USD','GBP','EUR')),
    balance_minor BIGINT NOT NULL DEFAULT 0,
    fees_minor    BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (merchant_id, currency)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restore the flat 00001+00002+00003 shape (DDL copied verbatim from those files)
-- so goose down lands exactly where 00003's Up left off. Data is not restored.

DROP TABLE IF EXISTS merchant_balances;
DROP TABLE IF EXISTS refunds;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS customers;
DROP TABLE IF EXISTS merchants;

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

ALTER TABLE payments ADD COLUMN network_fee_minor BIGINT;

CREATE INDEX IF NOT EXISTS payments_merchant_created_idx
    ON payments (merchant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS refunds (
    id            TEXT PRIMARY KEY,
    payment_id    TEXT NOT NULL REFERENCES payments(id),
    amount_minor  BIGINT NOT NULL CHECK (amount_minor > 0),
    currency      TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL
);

ALTER TABLE refunds
    ADD COLUMN status      TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','settled')),
    ADD COLUMN merchant_id TEXT NOT NULL DEFAULT '';

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

CREATE INDEX IF NOT EXISTS subs_status_next_charge_idx
    ON subscriptions (status, next_charge_at);

CREATE TABLE IF NOT EXISTS merchant_balances (
    merchant_id   TEXT   NOT NULL,
    currency      TEXT   NOT NULL CHECK (currency IN ('USD','GBP','EUR')),
    balance_minor BIGINT NOT NULL DEFAULT 0,
    fees_minor    BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (merchant_id, currency)
);

-- +goose StatementEnd
