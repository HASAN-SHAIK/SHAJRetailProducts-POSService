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

func TestPullUntilCaughtUpTraversesPagedCursorAndPersistsCheckpoint(t *testing.T) {
	db := testutil.OpenDatabase(t)
	inboxService := inbox.New(db)
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v1/sync/changes" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "100" {
			t.Fatalf("limit=%s", r.URL.Query().Get("limit"))
		}
		if r.Header.Get("X-POS-Device-ID") != "dev-page" {
			t.Fatalf("device header=%q", r.Header.Get("X-POS-Device-ID"))
		}
		if r.Header.Get("X-POS-Tenant-ID") != "tenant-page" {
			t.Fatalf("tenant header=%q", r.Header.Get("X-POS-Tenant-ID"))
		}
		if r.Header.Get("X-POS-Sync-Token") != "secret-page" {
			t.Fatalf("sync token header=%q", r.Header.Get("X-POS-Sync-Token"))
		}

		switch requests {
		case 1:
			if cursor := r.URL.Query().Get("cursor"); cursor != "" {
				t.Fatalf("first request cursor=%q", cursor)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cursor":   "cursor-page-1",
				"has_more": true,
				"changes": []map[string]any{{
					"id":             "product:multipage:1",
					"type":           "catalog.product.upsert",
					"schema_version": 1,
					"source":         "central",
					"payload": map[string]any{
						"id":                 "multipage-1",
						"name":               "Page One Product",
						"is_active":          true,
						"allow_manual_price": false,
						"track_inventory":    true,
						"version":            1,
					},
				}},
			})
		case 2:
			if cursor := r.URL.Query().Get("cursor"); cursor != "cursor-page-1" {
				t.Fatalf("second request cursor=%q", cursor)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cursor":   "cursor-page-2",
				"has_more": false,
				"changes": []map[string]any{{
					"id":             "product:multipage:2",
					"type":           "catalog.product.upsert",
					"schema_version": 1,
					"source":         "central",
					"payload": map[string]any{
						"id":                 "multipage-2",
						"name":               "Page Two Product",
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
	defer server.Close()

	puller := New(db, inboxService, server.URL, "tenant-page", "secret-page", "dev-page", time.Second, time.Second)
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
	if cursor != "cursor-page-2" {
		t.Fatalf("final cursor=%q", cursor)
	}

	var applied int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE message_id IN ('product:multipage:1','product:multipage:2') AND status='applied'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 2 {
		t.Fatalf("applied messages=%d want=2", applied)
	}

	var products int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM catalog_products WHERE id IN ('multipage-1','multipage-2') AND is_active=1`).Scan(&products); err != nil {
		t.Fatal(err)
	}
	if products != 2 {
		t.Fatalf("projected products=%d want=2", products)
	}

	var checkpointRows int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM sync_checkpoints WHERE stream_name='central_changes' AND cursor_value='cursor-page-2'`).Scan(&checkpointRows); err != nil {
		t.Fatal(err)
	}
	if checkpointRows != 1 {
		t.Fatalf("final checkpoint rows=%d want=1", checkpointRows)
	}
}
