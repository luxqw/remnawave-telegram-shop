-- Mark existing trial users: those with subscription_link but no paid purchases.
UPDATE customer
SET is_trial = TRUE
WHERE subscription_link IS NOT NULL
  AND id NOT IN (
      SELECT DISTINCT customer_id FROM purchase WHERE status = 'paid'
  );
