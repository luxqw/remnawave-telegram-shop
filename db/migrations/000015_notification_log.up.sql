CREATE TABLE notification_log (
    id                    BIGSERIAL PRIMARY KEY,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    customer_telegram_id  BIGINT NOT NULL,
    notification_type     TEXT NOT NULL,
    status                TEXT NOT NULL,
    detail                TEXT,
    error_message         TEXT,
    source                TEXT NOT NULL DEFAULT 'system'
);

CREATE INDEX idx_notification_log_customer ON notification_log (customer_telegram_id);
CREATE INDEX idx_notification_log_created_at ON notification_log (created_at DESC);
CREATE INDEX idx_notification_log_type ON notification_log (notification_type);
