-- WARNING: only safe to run immediately after the matching .up.sql, before any new tribute
-- purchases/top-ups are created — it blindly multiplies every tribute row by 100 again, which
-- would corrupt correctly-priced rows written after the up migration ran.
UPDATE purchase SET amount = amount * 100.0 WHERE invoice_type = 'tribute';
UPDATE traffic_topups SET price_amount = price_amount * 100.0 WHERE tribute_payment_id IS NOT NULL;
