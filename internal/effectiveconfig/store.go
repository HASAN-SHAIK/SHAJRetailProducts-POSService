package effectiveconfig

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

type Scope struct {
    TenantID string `json:"tenant_id,omitempty"`
    BranchID string `json:"branch_id,omitempty"`
    DeviceID string `json:"device_id,omitempty"`
}

type Snapshot struct {
    SchemaVersion int            `json:"schema_version"`
    GeneratedAt   string         `json:"generated_at,omitempty"`
    ETag          string         `json:"etag"`
    Scope         Scope          `json:"scope"`
    Values        map[string]any `json:"values"`
    Config        map[string]any `json:"config"`
    Sources       map[string]any `json:"sources,omitempty"`
    FetchedAt     string         `json:"fetched_at,omitempty"`
}

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

func (s *Store) Get(ctx context.Context) (Snapshot, error) {
    var payload, fetchedAt string
    err := s.db.SQL().QueryRowContext(ctx, `SELECT payload_json, fetched_at FROM effective_config_snapshot WHERE singleton_id = 1`).Scan(&payload, &fetchedAt)
    if err != nil { return Snapshot{}, err }
    var out Snapshot
    if err := json.Unmarshal([]byte(payload), &out); err != nil { return Snapshot{}, fmt.Errorf("decode effective config snapshot: %w", err) }
    out.FetchedAt = fetchedAt
    return out, nil
}

func (s *Store) Bool(ctx context.Context, key string, defaultValue bool) (bool, error) {
    snapshot, err := s.Get(ctx)
    if errors.Is(err, sql.ErrNoRows) { return defaultValue, nil }
    if err != nil { return false, err }
    value, ok := snapshot.Values[key]
    if !ok || value == nil { return defaultValue, nil }
    typed, ok := value.(bool)
    if !ok { return false, fmt.Errorf("effective configuration %s must be boolean", key) }
    return typed, nil
}

func (s *Store) Float64(ctx context.Context, key string, defaultValue float64) (float64, error) {
    snapshot, err := s.Get(ctx)
    if errors.Is(err, sql.ErrNoRows) { return defaultValue, nil }
    if err != nil { return 0, err }
    value, ok := snapshot.Values[key]
    if !ok || value == nil { return defaultValue, nil }
    typed, ok := value.(float64)
    if !ok { return 0, fmt.Errorf("effective configuration %s must be numeric", key) }
    return typed, nil
}

func (s *Store) Save(ctx context.Context, snapshot Snapshot) error {
    if snapshot.SchemaVersion <= 0 || snapshot.ETag == "" { return errors.New("effective configuration schema_version and etag are required") }
    raw, err := json.Marshal(snapshot)
    if err != nil { return fmt.Errorf("encode effective config snapshot: %w", err) }
    now := time.Now().UTC().Format(time.RFC3339Nano)
    _, err = s.db.SQL().ExecContext(ctx, `
        INSERT INTO effective_config_snapshot(singleton_id,schema_version,etag,tenant_id,branch_id,device_id,payload_json,source_generated_at,fetched_at,updated_at)
        VALUES(1,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(singleton_id) DO UPDATE SET schema_version=excluded.schema_version,etag=excluded.etag,tenant_id=excluded.tenant_id,branch_id=excluded.branch_id,device_id=excluded.device_id,payload_json=excluded.payload_json,source_generated_at=excluded.source_generated_at,fetched_at=excluded.fetched_at,updated_at=excluded.updated_at`,
        snapshot.SchemaVersion, snapshot.ETag, emptyAsNil(snapshot.Scope.TenantID), emptyAsNil(snapshot.Scope.BranchID), emptyAsNil(snapshot.Scope.DeviceID), string(raw), emptyAsNil(snapshot.GeneratedAt), now, now)
    if err != nil { return fmt.Errorf("save effective config snapshot: %w", err) }
    return nil
}

func emptyAsNil(value string) any { if value == "" { return nil }; return value }
