ALTER TABLE pos_manager_approvals ADD COLUMN order_id TEXT;

CREATE INDEX IF NOT EXISTS idx_pos_manager_approvals_order_scope
    ON pos_manager_approvals(cashier_user_id, permission, order_id, expires_at, consumed_at);
