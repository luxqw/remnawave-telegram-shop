CREATE TABLE webhook_inbox (
    id           BIGSERIAL PRIMARY KEY,
    payload      BYTEA       NOT NULL,
    event_type   TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL DEFAULT 'pending',
    attempts     INT         NOT NULL DEFAULT 0,
    error_msg    TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);
CREATE INDEX idx_webhook_inbox_status_attempts ON webhook_inbox (status, attempts);
