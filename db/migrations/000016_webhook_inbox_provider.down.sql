DROP INDEX IF EXISTS idx_webhook_inbox_provider_status_attempts;
ALTER TABLE webhook_inbox DROP COLUMN IF EXISTS provider;
