CREATE TABLE IF NOT EXISTS catalog_categories (
    id TEXT PRIMARY KEY,
    parent_id TEXT,
    name TEXT NOT NULL,
    code TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0,1)),
    version INTEGER NOT NULL DEFAULT 1,
    source_updated_at TEXT,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (parent_id) REFERENCES catalog_categories(id)
);

CREATE INDEX IF NOT EXISTS idx_catalog_categories_parent
    ON catalog_categories(parent_id, sort_order, name);

CREATE TABLE IF NOT EXISTS catalog_products (
    id TEXT PRIMARY KEY,
    category_id TEXT,
    sku TEXT,
    name TEXT NOT NULL,
    description TEXT,
    unit_of_measure TEXT,
    tax_code TEXT,
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0,1)),
    allow_manual_price INTEGER NOT NULL DEFAULT 0 CHECK (allow_manual_price IN (0,1)),
    track_inventory INTEGER NOT NULL DEFAULT 1 CHECK (track_inventory IN (0,1)),
    version INTEGER NOT NULL DEFAULT 1,
    source_updated_at TEXT,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (category_id) REFERENCES catalog_categories(id)
);

CREATE INDEX IF NOT EXISTS idx_catalog_products_category
    ON catalog_products(category_id, is_active, name);
CREATE INDEX IF NOT EXISTS idx_catalog_products_sku
    ON catalog_products(sku);

CREATE TABLE IF NOT EXISTS catalog_barcodes (
    barcode TEXT PRIMARY KEY,
    product_id TEXT NOT NULL,
    barcode_type TEXT,
    is_primary INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0,1)),
    updated_at TEXT NOT NULL,
    FOREIGN KEY (product_id) REFERENCES catalog_products(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_catalog_barcodes_product
    ON catalog_barcodes(product_id, is_primary DESC);

CREATE TABLE IF NOT EXISTS catalog_prices (
    id TEXT PRIMARY KEY,
    product_id TEXT NOT NULL,
    store_id TEXT,
    price_list_id TEXT,
    currency TEXT NOT NULL,
    amount_minor INTEGER NOT NULL CHECK (amount_minor >= 0),
    tax_inclusive INTEGER NOT NULL DEFAULT 1 CHECK (tax_inclusive IN (0,1)),
    valid_from TEXT,
    valid_to TEXT,
    priority INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    source_updated_at TEXT,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (product_id) REFERENCES catalog_products(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_catalog_prices_lookup
    ON catalog_prices(product_id, store_id, priority DESC, valid_from, valid_to);

CREATE TABLE IF NOT EXISTS catalog_taxes (
    tax_code TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    rate_bps INTEGER NOT NULL CHECK (rate_bps >= 0),
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0,1)),
    version INTEGER NOT NULL DEFAULT 1,
    source_updated_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS catalog_projection_state (
    singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
    generation INTEGER NOT NULL DEFAULT 0,
    last_full_sync_at TEXT,
    last_incremental_sync_at TEXT,
    source_cursor TEXT,
    updated_at TEXT NOT NULL
);
