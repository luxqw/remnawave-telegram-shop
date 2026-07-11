-- Tribute webhook amounts arrive in kopecks but were stored as whole rubles (bug fixed in the
-- Go code alongside this migration). Every tribute row present at migration time was written by
-- the buggy code, since this runs once, before the bot starts processing any new webhooks.
UPDATE purchase SET amount = amount / 100.0 WHERE invoice_type = 'tribute';
UPDATE traffic_topups SET price_amount = price_amount / 100.0 WHERE tribute_payment_id IS NOT NULL;
