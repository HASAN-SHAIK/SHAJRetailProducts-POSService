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

func TestPullUntilCaughtUpBoundsRepeatedHasMoreWithUnchangedCursor(t *testing.T) {
    db := testutil.OpenDatabase(t)
    inboxService := inbox.New(db)
    requests := 0

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        requests++
        if r.URL.Path != "/api/v1/sync/changes" {
            t.Errorf("path=%s", r.URL.Path)
        }
        if got := r.Header.Get("X-POS-Device-ID"); got != "dev-stall" {
            t.Errorf("device header=%q", got)
        }
        if got := r.Header.Get("X-POS-Tenant-ID"); got != "tenant-stall" {
            t.Errorf("tenant header=%q", got)
        }
        if got := r.Header.Get("X-POS-Sync-Token"); got != "secret-stall" {
            t.Errorf("token header=%q", got)
        }
        if requests == 1 {
            if got := r.URL.Query().Get("cursor"); got != "" {
                t.Errorf("first cursor=%q", got)
            }
        } else if got := r.URL.Query().Get("cursor"); got != "cursor-stalled" {
            t.Errorf("request %d cursor=%q", requests, got)
        }

        _ = json.NewEncoder(w).Encode(map[string]any{
            "cursor":   "cursor-stalled",
            "has_more": true,
            "changes": []map[string]any{{
                "id":             "product:stall:v1",
                "type":           "catalog.product.upsert",
                "schema_version": 1,
                "source":         "central",
                "payload": map[string]any{
                    "id":                 "stall-product",
                    "name":               "Stall Product",
                    "is_active":          true,
                    "allow_manual_price": false,
                    "track_inventory":    true,
                    "version":            1,
                },
            }},
        })
    }))
    defer server.Close()

    puller := New(db, inboxService, server.URL, "tenant-stall", "secret-stall", "dev-stall", time.Second, time.Second)
    if err := puller.pullUntilCaughtUp(context.Background()); err != nil {
        t.Fatalf("pullUntilCaughtUp returned error: %v", err)
    }

    if requests != 10 {
        t.Fatalf("stalled has_more loop requests=%d, want 10", requests)
    }

    cursor, err := puller.cursor(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    if cursor != "cursor-stalled" {
        t.Fatalf("checkpoint=%q, want cursor-stalled", cursor)
    }

    var applied int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id='product:stall:v1' AND status='applied'`).Scan(&applied); err != nil {
        t.Fatal(err)
    }
    if applied != 1 {
        t.Fatalf("applied inbox rows=%d, want 1", applied)
    }

    var products int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM catalog_products WHERE id='stall-product' AND name='Stall Product'`).Scan(&products); err != nil {
        t.Fatal(err)
    }
    if products != 1 {
        t.Fatalf("projected product rows=%d, want 1", products)
    }
}
