package effectiveconfig

import (
    "context"
    "path/filepath"
    "testing"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestSnapshotPersistsAcrossDatabaseReopen(t *testing.T) {
    ctx := context.Background()
    path := filepath.Join(t.TempDir(), "pos.db")

    db, err := database.Open(ctx, path)
    if err != nil { t.Fatal(err) }
    if err := db.Migrate(ctx); err != nil { t.Fatal(err) }
    store := NewStore(db)
    snapshot := Snapshot{
        SchemaVersion: 1,
        GeneratedAt: "2026-08-14T00:00:00Z",
        ETag: "etag-1",
        Scope: Scope{TenantID: "tenant-1", BranchID: "branch-1", DeviceID: "device-1"},
        Values: map[string]any{"offline.sales_enabled": true},
        Config: map[string]any{"offline": map[string]any{"sales_enabled": true}},
    }
    if err := store.Save(ctx, snapshot); err != nil { t.Fatal(err) }
    if err := store.RecordSuccess(ctx, snapshot.ETag); err != nil { t.Fatal(err) }
    if err := db.Close(); err != nil { t.Fatal(err) }

    db, err = database.Open(ctx, path)
    if err != nil { t.Fatal(err) }
    defer db.Close()
    if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

    got, err := NewStore(db).Get(ctx)
    if err != nil { t.Fatal(err) }
    if got.ETag != "etag-1" || got.Scope.DeviceID != "device-1" { t.Fatalf("unexpected snapshot: %+v", got) }
    offline, ok := got.Config["offline"].(map[string]any)
    if !ok || offline["sales_enabled"] != true { t.Fatalf("unexpected config: %+v", got.Config) }
}
