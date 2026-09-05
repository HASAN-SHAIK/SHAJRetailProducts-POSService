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

func TestPullUntilCaughtUpResumesFromLastSuccessfulPageAfterNextPageFailure(t *testing.T) {
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
        if got := r.Header.Get("X-POS-Device-ID"); got != "dev-recovery" {
            t.Errorf("device header=%q", got)
        }
        if got := r.Header.Get("X-POS-Tenant-ID"); got != "tenant-recovery" {
            t.Errorf("tenant header=%q", got)
        }
        if got := r.Header.Get("X-POS-Sync-Token"); got != "secret-recovery" {
            t.Errorf("token header=%q", got)
        }

        switch requests {
        case 1:
            _ = json.NewEncoder(w).Encode(map[string]any{
                "cursor":   "cursor-page-1",
                "has_more": true,
                "changes": []map[string]any{{
                    "id":             "product:recovery:first",
                    "type":           "catalog.product.upsert",
                    "schema_version": 1,
                    "source":         "central",
                    "payload": map[string]any{
                        "id":                 "recovery-first",
                        "name":               "Recovery First",
                        "is_active":          true,
                        "allow_manual_price": false,
                        "track_inventory":    true,
                        "version":            1,
                    },
                }},
            })
        case 2:
            if r.URL.Query().Get("cursor") != "cursor-page-1" {
                t.Errorf("failed-page cursor=%q", r.URL.Query().Get("cursor"))
            }
            http.Error(w, "temporary central failure", http.StatusServiceUnavailable)
        case 3:
            if r.URL.Query().Get("cursor") != "cursor-page-1" {
                t.Errorf("recovery cursor=%q", r.URL.Query().Get("cursor"))
            }
            _ = json.NewEncoder(w).Encode(map[string]any{
                "cursor":   "cursor-page-2",
                "has_more": false,
                "changes": []map[string]any{{
                    "id":             "product:recovery:second",
                    "type":           "catalog.product.upsert",
                    "schema_version": 1,
                    "source":         "central",
                    "payload": map[string]any{
                        "id":                 "recovery-second",
                        "name":               "Recovery Second",
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

    puller := New(db, inboxService, server.URL, "tenant-recovery", "secret-recovery", "dev-recovery", time.Second, time.Second)

    err := puller.pullUntilCaughtUp(context.Background())
    if err == nil || !strings.Contains(err.Error(), "change_feed_http_503") {
        t.Fatalf("expected second-page HTTP failure, got %v", err)
    }

    cursor, err := puller.cursor(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    if cursor != "cursor-page-1" {
        t.Fatalf("checkpoint after second-page failure=%q, want cursor-page-1", cursor)
    }

    var firstApplied int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id='product:recovery:first' AND status='applied'`).Scan(&firstApplied); err != nil {
        t.Fatal(err)
    }
    if firstApplied != 1 {
        t.Fatalf("first page applied rows=%d, want 1", firstApplied)
    }

    if err := puller.pullUntilCaughtUp(context.Background()); err != nil {
        t.Fatalf("recovery pull failed: %v", err)
    }

    if requests != 3 {
        t.Fatalf("requests=%d, want 3", requests)
    }
    if len(cursors) != 3 || cursors[0] != "" || cursors[1] != "cursor-page-1" || cursors[2] != "cursor-page-1" {
        t.Fatalf("cursor sequence=%v, want [<empty> cursor-page-1 cursor-page-1]", cursors)
    }

    cursor, err = puller.cursor(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    if cursor != "cursor-page-2" {
        t.Fatalf("final checkpoint=%q, want cursor-page-2", cursor)
    }

    var appliedMessages int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id IN ('product:recovery:first','product:recovery:second') AND status='applied'`).Scan(&appliedMessages); err != nil {
        t.Fatal(err)
    }
    if appliedMessages != 2 {
        t.Fatalf("applied inbox rows=%d, want 2", appliedMessages)
    }

    var products int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM catalog_products WHERE id IN ('recovery-first','recovery-second')`).Scan(&products); err != nil {
        t.Fatal(err)
    }
    if products != 2 {
        t.Fatalf("projected product rows=%d, want 2", products)
    }
}
