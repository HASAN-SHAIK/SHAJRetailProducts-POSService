CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS service_metadata (
    key TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS device_identity (
    singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
    device_id TEXT NOT NULL UNIQUE,
    store_id TEXT,
    terminal_id TEXT,
    installation_id TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'unregistered'
        CHECK (status IN ('unregistered','active','suspended','revoked')),
    registered_at TEXT,
    last_heartbeat_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS outbox_events (
    id TEXT PRIMARY KEY,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    aggregate_version INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL DEFAULT 1,
    ordering_key TEXT NOT NULL,
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','published','failed','dead_letter')),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    available_at TEXT NOT NULL,
    locked_at TEXT,
    lock_owner TEXT,
    last_error TEXT,
    created_at TEXT NOT NULL,
    published_at TEXT,
    UNIQUE (aggregate_type, aggregate_id, aggregate_version, event_type)
);

CREATE INDEX IF NOT EXISTS idx_outbox_dispatch
    ON outbox_events(status, available_at, created_at);
CREATE INDEX IF NOT EXISTS idx_outbox_ordering
    ON outbox_events(ordering_key, aggregate_version);

CREATE TABLE IF NOT EXISTS inbox_messages (
    message_id TEXT PRIMARY KEY,
    message_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL DEFAULT 1,
    source TEXT NOT NULL,
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    status TEXT NOT NULL DEFAULT 'received'
        CHECK (status IN ('received','processing','applied','failed')),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    received_at TEXT NOT NULL,
    applied_at TEXT,
    last_error TEXT
);

CREATE INDEX IF NOT EXISTS idx_inbox_status_received
    ON inbox_messages(status, received_at);

CREATE TABLE IF NOT EXISTS sync_checkpoints (
    stream_name TEXT PRIMARY KEY,
    cursor_value TEXT NOT NULL,
    source_updated_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS idempotency_records (
    scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_status INTEGER,
    response_json TEXT,
    created_at TEXT NOT NULL,
    expires_at TEXT,
    PRIMARY KEY (scope, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_expiry
    ON idempotency_records(expires_at);
