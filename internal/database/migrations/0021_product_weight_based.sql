ALTER TABLE catalog_products
    ADD COLUMN is_weight_based INTEGER NOT NULL DEFAULT 0
    CHECK (is_weight_based IN (0,1));
