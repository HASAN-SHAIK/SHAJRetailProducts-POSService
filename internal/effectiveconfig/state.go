package effectiveconfig

import (
    "context"
    "database/sql"
    "errors"
    "time"
)

type SyncState struct {
    LastAttemptAt *string `json:"last_attempt_at,omitempty"`
    LastSuccessAt *string `json:"last_success_at,omitempty"`
    LastError     *string `json:"last_error,omitempty"`
    LastETag      *string `json:"last_etag,omitempty"`
}

func (s *Store) RecordAttempt(ctx context.Context, syncErr error) error {
    now := time.Now().UTC().Format(time.RFC3339Nano)
    var message any
    if syncErr != nil { message = syncErr.Error() }
    _, err := s.db.SQL().ExecContext(ctx, `INSERT INTO effective_config_sync_state(singleton_id,last_attempt_at,last_error,updated_at) VALUES(1,?,?,?) ON CONFLICT(singleton_id) DO UPDATE SET last_attempt_at=excluded.last_attempt_at,last_error=excluded.last_error,updated_at=excluded.updated_at`, now, message, now)
    return err
}

func (s *Store) RecordSuccess(ctx context.Context, etag string) error {
    now := time.Now().UTC().Format(time.RFC3339Nano)
    _, err := s.db.SQL().ExecContext(ctx, `INSERT INTO effective_config_sync_state(singleton_id,last_attempt_at,last_success_at,last_error,last_etag,updated_at) VALUES(1,?,?,NULL,?,?) ON CONFLICT(singleton_id) DO UPDATE SET last_attempt_at=excluded.last_attempt_at,last_success_at=excluded.last_success_at,last_error=NULL,last_etag=excluded.last_etag,updated_at=excluded.updated_at`, now, now, etag, now)
    return err
}

func (s *Store) State(ctx context.Context) (SyncState, error) {
    var attempt, success, lastError, etag sql.NullString
    err := s.db.SQL().QueryRowContext(ctx, `SELECT last_attempt_at,last_success_at,last_error,last_etag FROM effective_config_sync_state WHERE singleton_id=1`).Scan(&attempt, &success, &lastError, &etag)
    if errors.Is(err, sql.ErrNoRows) { return SyncState{}, nil }
    if err != nil { return SyncState{}, err }
    return SyncState{LastAttemptAt: nullPtr(attempt), LastSuccessAt: nullPtr(success), LastError: nullPtr(lastError), LastETag: nullPtr(etag)}, nil
}

func nullPtr(value sql.NullString) *string { if !value.Valid { return nil }; v := value.String; return &v }
