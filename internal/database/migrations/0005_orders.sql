CREATE TABLE IF NOT EXISTS sales_orders (
    id TEXT PRIMARY KEY,
    client_order_id TEXT NOT NULL UNIQUE,
    store_id TEXT NOT NULL,
    terminal_id TEXT,
    customer_id TEXT REFERENCES customers(id),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','confirmed','partially_paid','paid','cancelled','returned')),
    currency TEXT NOT NULL DEFAULT 'INR',
    subtotal_minor INTEGER NOT NULL,
    discount_minor INTEGER NOT NULL DEFAULT 0,
    tax_minor INTEGER NOT NULL DEFAULT 0,
    total_minor INTEGER NOT NULL,
    notes TEXT,
    source TEXT NOT NULL DEFAULT 'pos',
    version INTEGER NOT NULL DEFAULT 1,
    completed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sales_orders_store_created
    ON sales_orders(store_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sales_orders_status
    ON sales_orders(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sales_orders_customer
    ON sales_orders(customer_id, created_at DESC);

CREATE TABLE IF NOT EXISTS sales_order_items (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL REFERENCES sales_orders(id) ON DELETE CASCADE,
    line_no INTEGER NOT NULL,
    product_id TEXT NOT NULL REFERENCES catalog_products(id),
    sku TEXT,
    product_name TEXT NOT NULL,
    barcode TEXT,
    quantity_milli INTEGER NOT NULL CHECK (quantity_milli > 0),
    unit_price_minor INTEGER NOT NULL CHECK (unit_price_minor >= 0),
    discount_minor INTEGER NOT NULL DEFAULT 0 CHECK (discount_minor >= 0),
    tax_minor INTEGER NOT NULL DEFAULT 0 CHECK (tax_minor >= 0),
    line_total_minor INTEGER NOT NULL CHECK (line_total_minor >= 0),
    tax_code TEXT,
    created_at TEXT NOT NULL,
    UNIQUE(order_id, line_no)
);

CREATE INDEX IF NOT EXISTS idx_sales_order_items_order ON sales_order_items(order_id, line_no);
CREATE INDEX IF NOT EXISTS idx_sales_order_items_product ON sales_order_items(product_id);

CREATE TABLE IF NOT EXISTS sales_order_snapshots (
    order_id TEXT NOT NULL REFERENCES sales_orders(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    snapshot_json TEXT NOT NULL CHECK (json_valid(snapshot_json)),
    created_at TEXT NOT NULL,
    PRIMARY KEY(order_id, version)
);

CREATE TABLE IF NOT EXISTS local_sequences (
    scope TEXT PRIMARY KEY,
    next_value INTEGER NOT NULL CHECK (next_value > 0),
    updated_at TEXT NOT NULL
);
