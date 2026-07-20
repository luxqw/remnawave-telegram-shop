ALTER TABLE webhook_inbox ADD COLUMN provider TEXT NOT NULL DEFAULT 'tribute';
CREATE INDEX idx_webhook_inbox_provider_status_attempts ON webhook_inbox (provider, status, attempts);
