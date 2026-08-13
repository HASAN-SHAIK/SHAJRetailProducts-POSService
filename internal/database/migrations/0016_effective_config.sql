CREATE TABLE IF NOT EXISTS effective_config_snapshot (
    singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
    schema_version INTEGER NOT NULL,
    etag TEXT NOT NULL,
    tenant_id TEXT,
    branch_id TEXT,
    device_id TEXT,
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    source_generated_at TEXT,
    fetched_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS effective_config_sync_state (
    singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
    last_attempt_at TEXT,
    last_success_at TEXT,
    last_error TEXT,
    last_etag TEXT,
    updated_at TEXT NOT NULL
);
