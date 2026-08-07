ALTER TABLE customers ADD COLUMN local_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE customers ADD COLUMN sync_state TEXT NOT NULL DEFAULT 'synced'
    CHECK (sync_state IN ('synced','pending','conflict'));

CREATE INDEX IF NOT EXISTS idx_customers_sync_state
    ON customers(sync_state, updated_at);
