CREATE TABLE traffic_topups (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT NOT NULL,
    remnawave_uuid TEXT NOT NULL DEFAULT '',
    gb_amount INTEGER NOT NULL,
    price_amount NUMERIC(12,2) NOT NULL,
    currency VARCHAR(10) NOT NULL,
    tribute_payment_id VARCHAR(255),
    target_traffic_limit_bytes BIGINT,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    UNIQUE (tribute_payment_id)
);
CREATE INDEX idx_traffic_topups_tg_id ON traffic_topups (telegram_id);
CREATE INDEX idx_traffic_topups_status ON traffic_topups (status);
