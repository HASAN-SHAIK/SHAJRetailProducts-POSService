CREATE TABLE IF NOT EXISTS inventory_balances (
    store_id TEXT NOT NULL,
    product_id TEXT NOT NULL REFERENCES catalog_products(id),
    on_hand_milli INTEGER NOT NULL DEFAULT 0,
    reserved_milli INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(store_id, product_id)
);

CREATE INDEX IF NOT EXISTS idx_inventory_balances_product
    ON inventory_balances(product_id, store_id);

CREATE TABLE IF NOT EXISTS inventory_movements (
    id TEXT PRIMARY KEY,
    store_id TEXT NOT NULL,
    product_id TEXT NOT NULL REFERENCES catalog_products(id),
    movement_type TEXT NOT NULL CHECK (movement_type IN (
        'purchase_receipt','sale_issue','sale_return','purchase_return',
        'adjustment_in','adjustment_out','transfer_in','transfer_out','opening'
    )),
    quantity_delta_milli INTEGER NOT NULL CHECK (quantity_delta_milli <> 0),
    reference_type TEXT,
    reference_id TEXT,
    order_item_id TEXT,
    reason TEXT,
    balance_after_milli INTEGER NOT NULL,
    occurred_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_inventory_movements_store_product
    ON inventory_movements(store_id, product_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_reference
    ON inventory_movements(reference_type, reference_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_inventory_sale_item_once
    ON inventory_movements(order_item_id, movement_type)
    WHERE order_item_id IS NOT NULL AND movement_type = 'sale_issue';
