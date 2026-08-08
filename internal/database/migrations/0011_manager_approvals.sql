CREATE TABLE IF NOT EXISTS pos_manager_approvals (
    token_hash BLOB PRIMARY KEY,
    cashier_user_id TEXT NOT NULL,
    approver_user_id TEXT NOT NULL,
    permission TEXT NOT NULL,
    reason TEXT,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_pos_manager_approvals_lookup
    ON pos_manager_approvals(cashier_user_id, permission, expires_at, consumed_at);

ALTER TABLE sales_orders ADD COLUMN approved_by_user_id TEXT;
ALTER TABLE sales_orders ADD COLUMN approval_reason TEXT;
