package changefeed

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestPullAppliesChangesAndAdvancesCheckpoint(t *testing.T) {
    db := testutil.OpenDatabase(t)
    inboxService := inbox.New(db)

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/v1/sync/changes" { t.Errorf("path=%s", r.URL.Path) }
        if r.Header.Get("X-POS-Device-ID") != "dev-1" { t.Errorf("device header") }
        if r.Header.Get("X-POS-Tenant-ID") != "tenant-1" { t.Errorf("tenant header") }
        if r.Header.Get("X-POS-Sync-Token") != "secret" { t.Errorf("token header") }
        _ = json.NewEncoder(w).Encode(map[string]any{
            "cursor": "cursor-1",
            "has_more": false,
            "changes": []map[string]any{{
                "id": "product:1:v1",
                "type": "catalog.product.upsert",
                "schema_version": 1,
                "source": "central",
                "payload": map[string]any{
                    "id": "1", "name": "Milk", "is_active": true,
                    "allow_manual_price": false, "track_inventory": true,
                    "version": 1,
                },
            }},
        })
    }))
    defer server.Close()

    puller := New(db, inboxService, server.URL, "tenant-1", "secret", "dev-1", time.Second, time.Second)
    more, err := puller.pullOnce(context.Background())
    if err != nil { t.Fatal(err) }
    if more { t.Fatal("unexpected has_more") }

    var productName string
    if err := db.SQL().QueryRow(`SELECT name FROM catalog_products WHERE id='1'`).Scan(&productName); err != nil { t.Fatal(err) }
    if productName != "Milk" { t.Fatalf("name=%s", productName) }

    var status string
    if err := db.SQL().QueryRow(`SELECT status FROM inbox_messages WHERE message_id='product:1:v1'`).Scan(&status); err != nil { t.Fatal(err) }
    if status != "applied" { t.Fatalf("inbox status=%s", status) }

    cursor, err := puller.cursor(context.Background())
    if err != nil { t.Fatal(err) }
    if cursor != "cursor-1" { t.Fatalf("cursor=%s", cursor) }
}

func TestCheckpointDoesNotAdvanceWhenChangeFails(t *testing.T) {
    db := testutil.OpenDatabase(t)
    inboxService := inbox.New(db)
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        _ = json.NewEncoder(w).Encode(map[string]any{
            "cursor": "cursor-bad",
            "has_more": false,
            "changes": []map[string]any{{"id":"bad-1","type":"unsupported.type","schema_version":1,"source":"central","payload":map[string]any{"id":"x"}}},
        })
    }))
    defer server.Close()

    puller := New(db, inboxService, server.URL, "tenant-1", "secret", "dev-1", time.Second, time.Second)
    if _, err := puller.pullOnce(context.Background()); err == nil { t.Fatal("expected apply failure") }
    cursor, err := puller.cursor(context.Background())
    if err != nil { t.Fatal(err) }
    if cursor != "" { t.Fatalf("checkpoint advanced after failed change: %s", cursor) }
}
