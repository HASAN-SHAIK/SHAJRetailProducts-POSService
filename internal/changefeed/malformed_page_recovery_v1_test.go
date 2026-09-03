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

func TestPullUntilCaughtUpRecoversFromMalformedNextPageWithoutAdvancingCheckpoint(t *testing.T) {
    db := testutil.OpenDatabase(t)
    inboxService := inbox.New(db)
    requests := 0
    cursors := make([]string, 0, 3)

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        requests++
        cursors = append(cursors, r.URL.Query().Get("cursor"))

        if r.URL.Path != "/api/v1/sync/changes" {
            t.Errorf("path=%s", r.URL.Path)
        }
        if got := r.Header.Get("X-POS-Device-ID"); got != "dev-malformed" {
            t.Errorf("device header=%q", got)
        }
        if got := r.Header.Get("X-POS-Tenant-ID"); got != "tenant-malformed" {
            t.Errorf("tenant header=%q", got)
        }
        if got := r.Header.Get("X-POS-Sync-Token"); got != "secret-malformed" {
            t.Errorf("token header=%q", got)
        }

        switch requests {
        case 1:
            _ = json.NewEncoder(w).Encode(map[string]any{
                "cursor":   "cursor-good-page",
                "has_more": true,
                "changes": []map[string]any{{
                    "id":             "product:malformed:first",
                    "type":           "catalog.product.upsert",
                    "schema_version": 1,
                    "source":         "central",
                    "payload": map[string]any{
                        "id":                 "malformed-first",
                        "name":               "Malformed First",
                        "is_active":          true,
                        "allow_manual_price": false,
                        "track_inventory":    true,
                        "version":            1,
                    },
                }},
            })
        case 2:
            if got := r.URL.Query().Get("cursor"); got != "cursor-good-page" {
                t.Errorf("malformed-page cursor=%q", got)
            }
            w.Header().Set("Content-Type", "application/json")
            _, _ = w.Write([]byte(`{"cursor":"cursor-must-not-persist","has_more":false,"changes":[`))
        case 3:
            if got := r.URL.Query().Get("cursor"); got != "cursor-good-page" {
                t.Errorf("recovery cursor=%q", got)
            }
            _ = json.NewEncoder(w).Encode(map[string]any{
                "cursor":   "cursor-recovered",
                "has_more": false,
                "changes": []map[string]any{{
                    "id":             "product:malformed:second",
                    "type":           "catalog.product.upsert",
                    "schema_version": 1,
                    "source":         "central",
                    "payload": map[string]any{
                        "id":                 "malformed-second",
                        "name":               "Malformed Second",
                        "is_active":          true,
                        "allow_manual_price": false,
                        "track_inventory":    true,
                        "version":            1,
                    },
                }},
            })
        default:
            t.Fatalf("unexpected request %d cursor=%q", requests, r.URL.Query().Get("cursor"))
        }
    }))
    defer server.Close()

    puller := New(db, inboxService, server.URL, "tenant-malformed", "secret-malformed", "dev-malformed", time.Second, time.Second)

    err := puller.pullUntilCaughtUp(context.Background())
    if err == nil || !strings.Contains(err.Error(), "decode change feed") {
        t.Fatalf("expected malformed JSON decode failure, got %v", err)
    }

    cursor, err := puller.cursor(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    if cursor != "cursor-good-page" {
        t.Fatalf("checkpoint after malformed page=%q, want cursor-good-page", cursor)
    }

    var firstApplied int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id='product:malformed:first' AND status='applied'`).Scan(&firstApplied); err != nil {
        t.Fatal(err)
    }
    if firstApplied != 1 {
        t.Fatalf("first page applied rows=%d, want 1", firstApplied)
    }

    var forbiddenCheckpoint int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM sync_checkpoints WHERE stream_name='central_changes' AND cursor_value='cursor-must-not-persist'`).Scan(&forbiddenCheckpoint); err != nil {
        t.Fatal(err)
    }
    if forbiddenCheckpoint != 0 {
        t.Fatalf("malformed response cursor was persisted: rows=%d", forbiddenCheckpoint)
    }

    if err := puller.pullUntilCaughtUp(context.Background()); err != nil {
        t.Fatalf("recovery pull failed: %v", err)
    }

    if requests != 3 {
        t.Fatalf("requests=%d, want 3", requests)
    }
    if len(cursors) != 3 || cursors[0] != "" || cursors[1] != "cursor-good-page" || cursors[2] != "cursor-good-page" {
        t.Fatalf("cursor sequence=%v, want [<empty> cursor-good-page cursor-good-page]", cursors)
    }

    cursor, err = puller.cursor(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    if cursor != "cursor-recovered" {
        t.Fatalf("final checkpoint=%q, want cursor-recovered", cursor)
    }

    var appliedMessages int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id IN ('product:malformed:first','product:malformed:second') AND status='applied'`).Scan(&appliedMessages); err != nil {
        t.Fatal(err)
    }
    if appliedMessages != 2 {
        t.Fatalf("applied inbox rows=%d, want 2", appliedMessages)
    }

    var products int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM catalog_products WHERE id IN ('malformed-first','malformed-second')`).Scan(&products); err != nil {
        t.Fatal(err)
    }
    if products != 2 {
        t.Fatalf("projected product rows=%d, want 2", products)
    }
}
