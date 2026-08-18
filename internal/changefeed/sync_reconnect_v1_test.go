package changefeed

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "sync/atomic"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/effectiveconfig"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
)

func TestV1ReconnectConvergesConfigAndChangeFeedAfterSQLiteRestart(t *testing.T) {
    ctx := context.Background()
    dbPath := filepath.Join(t.TempDir(), "pos.db")
    var generation atomic.Int32

    central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("X-POS-Tenant-ID") != "tenant-1" || r.Header.Get("X-POS-Device-ID") != "device-1" || r.Header.Get("X-POS-Sync-Token") != "sync-token" {
            t.Fatalf("unexpected machine identity headers: tenant=%q device=%q", r.Header.Get("X-POS-Tenant-ID"), r.Header.Get("X-POS-Device-ID"))
        }

        switch r.URL.Path {
        case "/api/v1/sync/config/effective":
            if generation.Load() == 0 {
                _ = json.NewEncoder(w).Encode(effectiveconfig.Snapshot{
                    SchemaVersion: 1,
                    ETag:          "etag-1",
                    Scope:         effectiveconfig.Scope{TenantID: "tenant-1", BranchID: "branch-a", DeviceID: "device-1"},
                    Values:        map[string]any{"offline.sales_enabled": true},
                    Config:        map[string]any{"offline": map[string]any{"sales_enabled": true}},
                })
                return
            }
            if r.Header.Get("If-None-Match") != `"etag-1"` {
                t.Fatalf("reconnect config refresh did not present retained etag: %q", r.Header.Get("If-None-Match"))
            }
            _ = json.NewEncoder(w).Encode(effectiveconfig.Snapshot{
                SchemaVersion: 1,
                ETag:          "etag-2",
                Scope:         effectiveconfig.Scope{TenantID: "tenant-1", BranchID: "branch-a", DeviceID: "device-1"},
                Values:        map[string]any{"offline.sales_enabled": false},
                Config:        map[string]any{"offline": map[string]any{"sales_enabled": false}},
            })

        case "/api/v1/sync/changes":
            cursor := r.URL.Query().Get("cursor")
            if generation.Load() == 0 {
                if cursor != "" { t.Fatalf("initial cursor=%q", cursor) }
                writeV1SyncPage(t, w, "cursor-1", "product:1:v1", 1, "Milk V1")
                return
            }
            if cursor == "cursor-1" {
                writeV1SyncPage(t, w, "cursor-2", "product:1:v2", 2, "Milk V2")
                return
            }
            if cursor == "cursor-2" {
                // Lost-ack/duplicate replay must remain side-effect free after reconnect.
                writeV1SyncPage(t, w, "cursor-2", "product:1:v2", 2, "Milk V2")
                return
            }
            t.Fatalf("unexpected reconnect cursor=%q", cursor)
        default:
            http.NotFound(w, r)
        }
    }))
    defer central.Close()

    db, err := database.Open(ctx, dbPath)
    if err != nil { t.Fatal(err) }
    if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

    configStore := effectiveconfig.NewStore(db)
    configClient, err := effectiveconfig.NewClient(central.URL, "tenant-1", "device-1", "sync-token", time.Second)
    if err != nil { t.Fatal(err) }
    configService := effectiveconfig.NewService(configStore, configClient, nil, time.Minute)
    puller := New(db, inbox.New(db), central.URL, "tenant-1", "sync-token", "device-1", time.Second, time.Minute)

    changed, err := configService.Refresh(ctx)
    if err != nil || !changed { t.Fatalf("initial config refresh changed=%v err=%v", changed, err) }
    if _, err := puller.pullOnce(ctx); err != nil { t.Fatal(err) }

    initialConfig, err := configStore.Get(ctx)
    if err != nil { t.Fatal(err) }
    if initialConfig.ETag != "etag-1" || initialConfig.Scope.BranchID != "branch-a" {
        t.Fatalf("initial config=%+v", initialConfig)
    }
    initialCursor, err := puller.cursor(ctx)
    if err != nil { t.Fatal(err) }
    if initialCursor != "cursor-1" { t.Fatalf("initial cursor=%q", initialCursor) }

    if err := db.Close(); err != nil { t.Fatal(err) }
    generation.Store(1)

    db, err = database.Open(ctx, dbPath)
    if err != nil { t.Fatal(err) }
    defer db.Close()
    if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

    restartedStore := effectiveconfig.NewStore(db)
    retained, err := restartedStore.Get(ctx)
    if err != nil { t.Fatal(err) }
    if retained.ETag != "etag-1" || retained.Scope.TenantID != "tenant-1" || retained.Scope.DeviceID != "device-1" {
        t.Fatalf("retained config after restart=%+v", retained)
    }

    restartedClient, err := effectiveconfig.NewClient(central.URL, "tenant-1", "device-1", "sync-token", time.Second)
    if err != nil { t.Fatal(err) }
    restartedConfig := effectiveconfig.NewService(restartedStore, restartedClient, nil, time.Minute)
    restartedPuller := New(db, inbox.New(db), central.URL, "tenant-1", "sync-token", "device-1", time.Second, time.Minute)

    retainedCursor, err := restartedPuller.cursor(ctx)
    if err != nil { t.Fatal(err) }
    if retainedCursor != "cursor-1" { t.Fatalf("cursor did not survive restart: %q", retainedCursor) }

    changed, err = restartedConfig.Refresh(ctx)
    if err != nil || !changed { t.Fatalf("reconnect config refresh changed=%v err=%v", changed, err) }
    if _, err := restartedPuller.pullOnce(ctx); err != nil { t.Fatal(err) }
    if _, err := restartedPuller.pullOnce(ctx); err != nil { t.Fatal(err) }

    convergedConfig, err := restartedStore.Get(ctx)
    if err != nil { t.Fatal(err) }
    if convergedConfig.ETag != "etag-2" || convergedConfig.Scope.TenantID != "tenant-1" || convergedConfig.Scope.BranchID != "branch-a" {
        t.Fatalf("converged config=%+v", convergedConfig)
    }
    enabled, err := restartedStore.Bool(ctx, "offline.sales_enabled", true)
    if err != nil { t.Fatal(err) }
    if enabled { t.Fatal("reconnect did not apply newer Central configuration") }

    finalCursor, err := restartedPuller.cursor(ctx)
    if err != nil { t.Fatal(err) }
    if finalCursor != "cursor-2" { t.Fatalf("final cursor=%q", finalCursor) }

    var productName string
    var productVersion int
    if err := db.SQL().QueryRow(`SELECT name, version FROM catalog_products WHERE id='1'`).Scan(&productName, &productVersion); err != nil { t.Fatal(err) }
    if productName != "Milk V2" || productVersion != 2 {
        t.Fatalf("catalog did not converge after reconnect: name=%q version=%d", productName, productVersion)
    }

    var appliedCount int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id='product:1:v2' AND status='applied'`).Scan(&appliedCount); err != nil { t.Fatal(err) }
    if appliedCount != 1 { t.Fatalf("duplicate reconnect replay reapplied inbox message: count=%d", appliedCount) }
}

func writeV1SyncPage(t *testing.T, w http.ResponseWriter, cursor, messageID string, version int, name string) {
    t.Helper()
    _ = json.NewEncoder(w).Encode(map[string]any{
        "cursor": cursor,
        "has_more": false,
        "changes": []map[string]any{{
            "id": messageID,
            "type": "catalog.product.upsert",
            "schema_version": 1,
            "source": "central",
            "payload": map[string]any{
                "id": "1",
                "name": name,
                "is_active": true,
                "allow_manual_price": false,
                "track_inventory": true,
                "version": version,
            },
        }},
    })
}
