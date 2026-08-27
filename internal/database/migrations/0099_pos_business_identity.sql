ALTER TABLE device_identity ADD COLUMN store_number TEXT;
ALTER TABLE device_identity ADD COLUMN pos_no TEXT;
ALTER TABLE device_identity ADD COLUMN touchpoint_id TEXT;

-- Preserve the legacy terminal value as the initial POS number where possible.
UPDATE device_identity
SET pos_no = COALESCE(NULLIF(pos_no, ''), NULLIF(terminal_id, ''))
WHERE singleton_id = 1;
