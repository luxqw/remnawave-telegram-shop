CREATE TABLE admin_audit_log (
    id                  BIGSERIAL PRIMARY KEY,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    admin_telegram_id   BIGINT NOT NULL,
    action              TEXT NOT NULL,
    target_telegram_id  BIGINT NOT NULL,
    param_int           INTEGER,
    outcome             TEXT NOT NULL,
    error_message       TEXT,
    source              TEXT NOT NULL DEFAULT 'command'
);

CREATE INDEX idx_admin_audit_log_target ON admin_audit_log (target_telegram_id);
CREATE INDEX idx_admin_audit_log_created_at ON admin_audit_log (created_at DESC);
