-- +goose Up
-- +goose StatementBegin

-- Tasks 51-55, 60-66: settlement + per-currency merchant balances.

-- Refunds gain a settlement lifecycle (created -> settled) and a denormalised
-- merchant_id so SettleRefund can hit the balance row without a join.
ALTER TABLE refunds
    ADD COLUMN status      TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','settled')),
    ADD COLUMN merchant_id TEXT NOT NULL DEFAULT '';

-- Backfill merchant_id for any pre-existing refunds from their payment.
UPDATE refunds r
   SET merchant_id = p.merchant_id
  FROM payments p
 WHERE r.payment_id = p.id;

-- Per-merchant per-currency ledger. bigint minor units (task 28 standard).
-- balance_minor: what the merchant is owed (settled net of fees and settled refunds).
-- fees_minor: lifetime fees paid to Gripe in that currency; revenue report sums this
-- (deliberately no fee_ledger table). PK doubles as the unique (merchant_id, currency).
CREATE TABLE IF NOT EXISTS merchant_balances (
    merchant_id   TEXT   NOT NULL,
    currency      TEXT   NOT NULL CHECK (currency IN ('USD','GBP','EUR')),
    balance_minor BIGINT NOT NULL DEFAULT 0,
    fees_minor    BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (merchant_id, currency)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS merchant_balances;
ALTER TABLE refunds DROP COLUMN IF EXISTS merchant_id, DROP COLUMN IF EXISTS status;
-- +goose StatementEnd
