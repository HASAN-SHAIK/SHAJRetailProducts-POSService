CREATE TABLE IF NOT EXISTS pos_sync_recoveries (
    recovery_id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    ordering_key TEXT NOT NULL,
    order_id TEXT NOT NULL,
    approved_by_user_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    consumed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pos_sync_recoveries_event
    ON pos_sync_recoveries(event_id, ordering_key);
