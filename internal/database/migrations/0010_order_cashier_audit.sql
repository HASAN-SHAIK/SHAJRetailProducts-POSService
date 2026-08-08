ALTER TABLE sales_orders ADD COLUMN created_by_user_id TEXT;
ALTER TABLE sales_orders ADD COLUMN completed_by_user_id TEXT;
ALTER TABLE payments ADD COLUMN created_by_user_id TEXT;
ALTER TABLE receipts ADD COLUMN issued_by_user_id TEXT;

CREATE INDEX IF NOT EXISTS idx_sales_orders_created_by_user
    ON sales_orders(created_by_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_sales_orders_completed_by_user
    ON sales_orders(completed_by_user_id, completed_at DESC);

CREATE INDEX IF NOT EXISTS idx_payments_created_by_user
    ON payments(created_by_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_receipts_issued_by_user
    ON receipts(issued_by_user_id, issued_at DESC);
