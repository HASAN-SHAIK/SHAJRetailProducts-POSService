package effectiveconfig

import (
    "context"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestV1RevokedCentralConfigRefreshPreservesLastAcceptedOfflineSnapshot(t *testing.T) {
    ctx := context.Background()
    path := filepath.Join(t.TempDir(), "pos.db")

    db, err := database.Open(ctx, path)
    if err != nil { t.Fatal(err) }
    if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

    store := NewStore(db)
    accepted := Snapshot{
        SchemaVersion: 1,
        GeneratedAt:   "2026-08-15T00:00:00Z",
        ETag:          "accepted-etag",
        Scope:         Scope{TenantID: "tenant-1", BranchID: "branch-a", DeviceID: "device-1"},
        Values:        map[string]any{"offline.sales_enabled": true},
        Config:        map[string]any{"offline": map[string]any{"sales_enabled": true}},
    }
    if err := store.Save(ctx, accepted); err != nil { t.Fatal(err) }
    if err := store.RecordSuccess(ctx, accepted.ETag); err != nil { t.Fatal(err) }

    central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("X-POS-Tenant-ID") != "tenant-1" || r.Header.Get("X-POS-Device-ID") != "device-1" {
            t.Fatalf("unexpected machine identity headers: tenant=%q device=%q", r.Header.Get("X-POS-Tenant-ID"), r.Header.Get("X-POS-Device-ID"))
        }
        if r.Header.Get("If-None-Match") != `"accepted-etag"` {
            t.Fatalf("expected refresh to present retained etag, got %q", r.Header.Get("If-None-Match"))
        }
        w.WriteHeader(http.StatusForbidden)
        _, _ = w.Write([]byte(`{"code":"POS_DEVICE_NOT_REGISTERED","message":"device registration is inactive"}`))
    }))
    defer central.Close()

    client, err := NewClient(central.URL, "tenant-1", "device-1", "sync-token", time.Second)
    if err != nil { t.Fatal(err) }
    service := NewService(store, client, nil, time.Minute)

    changed, err := service.Refresh(ctx)
    if err == nil || !strings.Contains(err.Error(), "returned 403") {
        t.Fatalf("expected revoked-device refresh to fail closed with 403, changed=%v err=%v", changed, err)
    }
    if changed { t.Fatal("revoked-device refresh must not replace the accepted snapshot") }

    retained, err := store.Get(ctx)
    if err != nil { t.Fatal(err) }
    if retained.ETag != accepted.ETag || retained.Scope.BranchID != "branch-a" || retained.Scope.DeviceID != "device-1" {
        t.Fatalf("accepted offline snapshot was mutated after revocation response: %+v", retained)
    }
    enabled, err := store.Bool(ctx, "offline.sales_enabled", false)
    if err != nil { t.Fatal(err) }
    if !enabled { t.Fatal("last accepted offline policy must remain readable after Central revocation response") }

    state, err := store.State(ctx)
    if err != nil { t.Fatal(err) }
    if state.LastError == nil || !strings.Contains(*state.LastError, "returned 403") {
        t.Fatalf("expected revocation refresh failure to remain support-visible, got %+v", state)
    }

    if err := db.Close(); err != nil { t.Fatal(err) }
    db, err = database.Open(ctx, path)
    if err != nil { t.Fatal(err) }
    defer db.Close()
    if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

    restarted, err := NewStore(db).Get(ctx)
    if err != nil { t.Fatal(err) }
    if restarted.ETag != accepted.ETag || restarted.Scope.DeviceID != "device-1" {
        t.Fatalf("accepted offline identity/config did not survive restart: %+v", restarted)
    }
}
