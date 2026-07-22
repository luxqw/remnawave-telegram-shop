CREATE TABLE admin_messages (
    id                   BIGSERIAL PRIMARY KEY,
    customer_telegram_id BIGINT NOT NULL,
    direction            VARCHAR(8) NOT NULL,
    text                 TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_admin_messages_customer ON admin_messages (customer_telegram_id, created_at);
