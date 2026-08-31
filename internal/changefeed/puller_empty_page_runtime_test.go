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

func TestPullUntilCaughtUpAdvancesCursorAcrossEmptyPage(t *testing.T) {
	db := testutil.OpenDatabase(t)
	inboxService := inbox.New(db)
	requests := 0

	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v1/sync/changes" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "100" {
			t.Fatalf("limit=%q", r.URL.Query().Get("limit"))
		}
		if r.Header.Get("X-POS-Device-ID") != "dev-empty" {
			t.Fatalf("device header=%q", r.Header.Get("X-POS-Device-ID"))
		}
		if r.Header.Get("X-POS-Tenant-ID") != "tenant-empty" {
			t.Fatalf("tenant header=%q", r.Header.Get("X-POS-Tenant-ID"))
		}
		if r.Header.Get("X-POS-Sync-Token") != "secret-empty" {
			t.Fatalf("sync token header=%q", r.Header.Get("X-POS-Sync-Token"))
		}

		switch requests {
		case 1:
			if cursor := r.URL.Query().Get("cursor"); cursor != "" {
				t.Fatalf("first request cursor=%q", cursor)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cursor":   "cursor-empty-page",
				"has_more": true,
				"changes":  []map[string]any{},
			})
		case 2:
			if cursor := r.URL.Query().Get("cursor"); cursor != "cursor-empty-page" {
				t.Fatalf("second request cursor=%q want=%q", cursor, "cursor-empty-page")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cursor":   "cursor-after-empty",
				"has_more": false,
				"changes": []map[string]any{{
					"id":             "product:after-empty:v1",
					"type":           "catalog.product.upsert",
					"schema_version": 1,
					"source":         "central",
					"payload": map[string]any{
						"id":                 "after-empty",
						"name":               "After Empty Page",
						"is_active":          true,
						"allow_manual_price": false,
						"track_inventory":    true,
						"version":            1,
					},
				}},
			})
		default:
			t.Fatalf("unexpected extra change-feed request %d", requests)
		}
	}))
	defer central.Close()

	puller := New(db, inboxService, central.URL, "tenant-empty", "secret-empty", "dev-empty", time.Second, time.Second)
	if err := puller.pullUntilCaughtUp(context.Background()); err != nil {
		t.Fatalf("pullUntilCaughtUp: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests=%d want=2", requests)
	}

	cursor, err := puller.cursor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "cursor-after-empty" {
		t.Fatalf("final cursor=%q want=%q", cursor, "cursor-after-empty")
	}

	var productName string
	if err := db.SQL().QueryRow(`SELECT name FROM catalog_products WHERE id='after-empty' AND is_active=1`).Scan(&productName); err != nil {
		t.Fatal(err)
	}
	if productName != "After Empty Page" {
		t.Fatalf("product name=%q", productName)
	}

	var applied int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id='product:after-empty:v1' AND status='applied'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("applied messages=%d want=1", applied)
	}

	var checkpoints int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM sync_checkpoints WHERE stream_name='central_changes' AND cursor_value='cursor-after-empty'`).Scan(&checkpoints); err != nil {
		t.Fatal(err)
	}
	if checkpoints != 1 {
		t.Fatalf("final checkpoint rows=%d want=1", checkpoints)
	}
}
