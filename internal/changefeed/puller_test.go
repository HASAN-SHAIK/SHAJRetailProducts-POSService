package changefeed

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
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

func TestFutureSchemaFailsClosedWithoutProjectionOrCursorAdvance(t *testing.T) {
    db := testutil.OpenDatabase(t)
    inboxService := inbox.New(db)
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        _ = json.NewEncoder(w).Encode(map[string]any{
            "cursor": "cursor-future",
            "has_more": false,
            "changes": []map[string]any{{
                "id": "product:future:v2",
                "type": "catalog.product.upsert",
                "schema_version": 2,
                "source": "central",
                "payload": map[string]any{
                    "id": "future-1", "name": "Future Milk", "is_active": true,
                    "allow_manual_price": false, "track_inventory": true,
                    "version": 2,
                },
            }},
        })
    }))
    defer server.Close()

    puller := New(db, inboxService, server.URL, "tenant-1", "secret", "dev-1", time.Second, time.Second)
    _, err := puller.pullOnce(context.Background())
    if err == nil || !strings.Contains(err.Error(), "unsupported_change_schema:catalog.product.upsert:v2") {
        t.Fatalf("expected explicit future schema rejection, got %v", err)
    }

    cursor, err := puller.cursor(context.Background())
    if err != nil { t.Fatal(err) }
    if cursor != "" { t.Fatalf("checkpoint advanced after future schema rejection: %s", cursor) }

    var products int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM catalog_products WHERE id='future-1'`).Scan(&products); err != nil { t.Fatal(err) }
    if products != 0 { t.Fatalf("future schema mutated catalog: count=%d", products) }

    var inboxRows int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id='product:future:v2'`).Scan(&inboxRows); err != nil { t.Fatal(err) }
    if inboxRows != 0 { t.Fatalf("future schema entered V1 inbox: count=%d", inboxRows) }
}

func TestPartialPageReplaySkipsAppliedMessageAndAdvancesOnlyAfterRecovery(t *testing.T) {
    db := testutil.OpenDatabase(t)
    inboxService := inbox.New(db)
    requests := 0

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        requests++
        secondSchema := 2
        if requests > 1 { secondSchema = 1 }
        _ = json.NewEncoder(w).Encode(map[string]any{
            "cursor": "cursor-page-1",
            "has_more": false,
            "changes": []map[string]any{
                {
                    "id": "product:page:first",
                    "type": "catalog.product.upsert",
                    "schema_version": 1,
                    "source": "central",
                    "payload": map[string]any{
                        "id": "page-first", "name": "First", "is_active": true,
                        "allow_manual_price": false, "track_inventory": true, "version": 1,
                    },
                },
                {
                    "id": "product:page:second",
                    "type": "catalog.product.upsert",
                    "schema_version": secondSchema,
                    "source": "central",
                    "payload": map[string]any{
                        "id": "page-second", "name": "Second", "is_active": true,
                        "allow_manual_price": false, "track_inventory": true, "version": 1,
                    },
                },
            },
        })
    }))
    defer server.Close()

    puller := New(db, inboxService, server.URL, "tenant-1", "secret", "dev-1", time.Second, time.Second)
    if _, err := puller.pullOnce(context.Background()); err == nil { t.Fatal("expected partial-page future schema failure") }

    cursor, err := puller.cursor(context.Background())
    if err != nil { t.Fatal(err) }
    if cursor != "" { t.Fatalf("cursor advanced after partial page: %s", cursor) }

    var firstApplied int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id='product:page:first' AND status='applied'`).Scan(&firstApplied); err != nil { t.Fatal(err) }
    if firstApplied != 1 { t.Fatalf("first message was not durably applied: %d", firstApplied) }

    if _, err := puller.pullOnce(context.Background()); err != nil { t.Fatalf("replay after recovery failed: %v", err) }
    cursor, err = puller.cursor(context.Background())
    if err != nil { t.Fatal(err) }
    if cursor != "cursor-page-1" { t.Fatalf("cursor did not advance after page recovery: %s", cursor) }

    var appliedMessages int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id IN ('product:page:first','product:page:second') AND status='applied'`).Scan(&appliedMessages); err != nil { t.Fatal(err) }
    if appliedMessages != 2 { t.Fatalf("page did not converge exactly once: applied=%d", appliedMessages) }

    var products int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM catalog_products WHERE id IN ('page-first','page-second')`).Scan(&products); err != nil { t.Fatal(err) }
    if products != 2 { t.Fatalf("catalog did not converge after partial-page replay: products=%d", products) }
}
