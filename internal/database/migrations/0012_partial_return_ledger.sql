CREATE TABLE IF NOT EXISTS pos_partial_returns (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL REFERENCES sales_orders(id),
    approved_by_user_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    refund_minor INTEGER NOT NULL CHECK(refund_minor >= 0),
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pos_partial_returns_order
    ON pos_partial_returns(order_id, created_at, id);

CREATE TABLE IF NOT EXISTS pos_partial_return_lines (
    return_id TEXT NOT NULL REFERENCES pos_partial_returns(id) ON DELETE CASCADE,
    order_id TEXT NOT NULL REFERENCES sales_orders(id),
    order_item_id TEXT NOT NULL REFERENCES sales_order_items(id),
    quantity_milli INTEGER NOT NULL CHECK(quantity_milli > 0),
    refund_minor INTEGER NOT NULL CHECK(refund_minor >= 0),
    created_at TEXT NOT NULL,
    PRIMARY KEY(return_id, order_item_id)
);

CREATE INDEX IF NOT EXISTS idx_pos_partial_return_lines_history
    ON pos_partial_return_lines(order_id, order_item_id, created_at, return_id);
