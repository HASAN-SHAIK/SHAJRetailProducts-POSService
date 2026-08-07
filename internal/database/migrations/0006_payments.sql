CREATE TABLE IF NOT EXISTS payments (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL REFERENCES sales_orders(id) ON DELETE CASCADE,
    client_payment_id TEXT NOT NULL UNIQUE,
    mode TEXT NOT NULL CHECK (mode IN ('cash','bank','upi','card','credit','wallet')),
    direction TEXT NOT NULL DEFAULT 'in' CHECK (direction IN ('in','out')),
    amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
    currency TEXT NOT NULL DEFAULT 'INR',
    status TEXT NOT NULL DEFAULT 'captured' CHECK (status IN ('pending','captured','failed','voided','refunded')),
    reference TEXT,
    provider TEXT,
    provider_payload_json TEXT CHECK (provider_payload_json IS NULL OR json_valid(provider_payload_json)),
    recorded_by TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_payments_order_created
    ON payments(order_id, created_at);
CREATE INDEX IF NOT EXISTS idx_payments_status
    ON payments(status, created_at);
CREATE INDEX IF NOT EXISTS idx_payments_mode
    ON payments(mode, created_at);

CREATE TABLE IF NOT EXISTS payment_snapshots (
    payment_id TEXT NOT NULL REFERENCES payments(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    snapshot_json TEXT NOT NULL CHECK (json_valid(snapshot_json)),
    created_at TEXT NOT NULL,
    PRIMARY KEY(payment_id, version)
);
