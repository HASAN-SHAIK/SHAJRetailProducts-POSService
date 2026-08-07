CREATE TABLE IF NOT EXISTS receipts (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL UNIQUE REFERENCES sales_orders(id) ON DELETE RESTRICT,
    receipt_number TEXT NOT NULL UNIQUE,
    document_type TEXT NOT NULL DEFAULT 'receipt' CHECK (document_type IN ('receipt','invoice')),
    store_id TEXT NOT NULL,
    terminal_id TEXT,
    customer_id TEXT,
    currency TEXT NOT NULL,
    total_minor INTEGER NOT NULL,
    paid_minor INTEGER NOT NULL DEFAULT 0,
    balance_minor INTEGER NOT NULL DEFAULT 0,
    snapshot_json TEXT NOT NULL CHECK (json_valid(snapshot_json)),
    snapshot_sha256 TEXT NOT NULL,
    issued_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_receipts_store_issued
    ON receipts(store_id, issued_at DESC);
CREATE INDEX IF NOT EXISTS idx_receipts_customer
    ON receipts(customer_id, issued_at DESC);
