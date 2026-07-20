ALTER TABLE traffic_topups ADD COLUMN rollypay_payment_id VARCHAR(255);
ALTER TABLE traffic_topups ADD CONSTRAINT traffic_topups_rollypay_payment_id_key UNIQUE (rollypay_payment_id);
