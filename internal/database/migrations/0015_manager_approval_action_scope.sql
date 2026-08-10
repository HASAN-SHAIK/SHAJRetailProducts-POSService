ALTER TABLE pos_manager_approvals ADD COLUMN action_scope TEXT;

CREATE INDEX IF NOT EXISTS idx_pos_manager_approvals_action_scope
    ON pos_manager_approvals(cashier_user_id, permission, order_id, action_scope, expires_at, consumed_at);
