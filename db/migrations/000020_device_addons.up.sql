CREATE TABLE device_addons (
    id               BIGSERIAL PRIMARY KEY,
    telegram_id      BIGINT NOT NULL,
    device_count     INTEGER NOT NULL,
    billing_mode     VARCHAR(16) NOT NULL,
    cycle_expires_at TIMESTAMPTZ NOT NULL,
    grace_until      TIMESTAMPTZ,
    status           VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_device_addons_tg_id ON device_addons (telegram_id);
CREATE INDEX idx_device_addons_status ON device_addons (status);
