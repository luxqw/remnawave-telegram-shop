ALTER TABLE traffic_topups DROP CONSTRAINT IF EXISTS traffic_topups_rollypay_payment_id_key;
ALTER TABLE traffic_topups DROP COLUMN IF EXISTS rollypay_payment_id;
