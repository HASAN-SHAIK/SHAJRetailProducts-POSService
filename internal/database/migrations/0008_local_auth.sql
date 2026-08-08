CREATE TABLE IF NOT EXISTS local_users (
    user_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    role TEXT NOT NULL,
    branch_id TEXT,
    all_branch_access INTEGER NOT NULL DEFAULT 0,
    permissions_json TEXT NOT NULL CHECK (json_valid(permissions_json)),
    pin_salt BLOB NOT NULL,
    pin_hash BLOB NOT NULL,
    pin_iterations INTEGER NOT NULL,
    grant_id TEXT NOT NULL,
    grant_expires_at TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_local_users_tenant_branch
    ON local_users(tenant_id, branch_id);

CREATE TABLE IF NOT EXISTS local_auth_sessions (
    token_hash BLOB PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES local_users(user_id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_local_auth_sessions_user
    ON local_auth_sessions(user_id, expires_at);
