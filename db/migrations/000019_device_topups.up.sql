CREATE TABLE device_topups (
    id                  BIGSERIAL PRIMARY KEY,
    telegram_id         BIGINT NOT NULL,
    remnawave_uuid      TEXT NOT NULL DEFAULT '',
    device_count        INTEGER NOT NULL DEFAULT 1,
    price_amount        NUMERIC(12,2) NOT NULL,
    currency            VARCHAR(10) NOT NULL,
    rollypay_payment_id VARCHAR(255),
    target_device_limit INTEGER,
    status              VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    UNIQUE (rollypay_payment_id)
);
CREATE INDEX idx_device_topups_tg_id ON device_topups (telegram_id);
CREATE INDEX idx_device_topups_status ON device_topups (status);
