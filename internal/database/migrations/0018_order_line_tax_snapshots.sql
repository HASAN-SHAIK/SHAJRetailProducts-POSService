ALTER TABLE sales_order_items
    ADD COLUMN taxable_minor INTEGER;

ALTER TABLE sales_order_items
    ADD COLUMN gst_rate_bps INTEGER
    CHECK (gst_rate_bps IS NULL OR (gst_rate_bps >= 0 AND gst_rate_bps <= 10000));
