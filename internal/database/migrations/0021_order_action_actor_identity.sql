ALTER TABLE sales_orders ADD COLUMN refunded_by_user_id TEXT;
ALTER TABLE sales_orders ADD COLUMN voided_by_user_id TEXT;

CREATE INDEX IF NOT EXISTS idx_sales_orders_refunded_by_user_id
    ON sales_orders(refunded_by_user_id);
CREATE INDEX IF NOT EXISTS idx_sales_orders_voided_by_user_id
    ON sales_orders(voided_by_user_id);
