-- +goose Up
-- +goose StatementBegin

-- Fee-worker: mock card-network fee, set exactly once per payment.
-- NULL = not yet set; SettleNetworkFee only fills NULLs, so SQS redeliveries no-op.
ALTER TABLE payments ADD COLUMN network_fee_minor BIGINT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE payments DROP COLUMN IF EXISTS network_fee_minor;
-- +goose StatementEnd
