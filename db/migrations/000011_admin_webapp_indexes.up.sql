CREATE INDEX idx_purchase_status ON purchase (status);
CREATE INDEX idx_purchase_customer_id ON purchase (customer_id);
CREATE INDEX idx_purchase_created_at ON purchase (created_at DESC);
CREATE INDEX idx_webhook_inbox_status ON webhook_inbox (status);
CREATE INDEX idx_admin_audit_log_admin ON admin_audit_log (admin_telegram_id);
CREATE INDEX idx_admin_audit_log_action ON admin_audit_log (action);
CREATE INDEX idx_customer_expire_at ON customer (expire_at);
