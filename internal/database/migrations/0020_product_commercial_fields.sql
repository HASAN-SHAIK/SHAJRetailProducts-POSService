ALTER TABLE catalog_products
    ADD COLUMN company TEXT;

ALTER TABLE catalog_products
    ADD COLUMN mrp REAL;

ALTER TABLE catalog_products
    ADD COLUMN expiry_date TEXT;
