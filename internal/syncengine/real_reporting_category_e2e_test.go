package syncengine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

func TestRealCentralCategorySnapshotE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" {
		t.Skip("POS_E2E_CENTRAL_URL is not set")
	}

	ctx := context.Background()
	tenantID := envOr("POS_E2E_TENANT_ID", "tenant-e2e")
	syncToken := envOr("POS_E2E_SYNC_TOKEN", "sync-secret")
	deviceID := envOr("POS_E2E_DEVICE_ID", "device-e2e")
	dbPath := filepath.Join(t.TempDir(), "pos-reporting-category-e2e.db")

	db, err := database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open POS database: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("migrate POS database: %v", err)
	}

	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_categories(id,name,sort_order,is_active,version,updated_at)
		VALUES(?,?,?,?,?,?)`,
		"cat-beverages", "Beverages", 1, 1, 1, "2026-08-18T10:00:00Z",
	); err != nil {
		t.Fatalf("insert projected category: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(id,category_id,name,is_active,allow_manual_price,track_inventory,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		"101", "cat-beverages", "Cola", 1, 0, 0, 1, "2026-08-18T10:00:00Z",
	); err != nil {
		t.Fatalf("insert projected product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"price-101", "101", "store-e2e", "INR", 10000, 1, 100, 1, "2026-08-18T10:00:00Z",
	); err != nil {
		t.Fatalf("insert projected price: %v", err)
	}

	orderService := orders.New(db, catalog.NewRepository(db))
	order, err := orderService.Create(ctx, orders.CreateInput{
		ClientOrderID: "category-reporting-order-e2e",
		StoreID:       "store-e2e",
		Currency:      "INR",
		Items: []orders.ItemInput{{
			ProductID:     orders.ExternalID("101"),
			QuantityMilli: 1000,
		}},
	})
	if err != nil {
		t.Fatalf("create offline POS order: %v", err)
	}
	if len(order.Items) != 1 || order.Items[0].CategoryNameSnapshot == nil || *order.Items[0].CategoryNameSnapshot != "Beverages" {
		t.Fatalf("sale-time category snapshot missing: %+v", order.Items)
	}

	// The current catalog changes after the offline sale was created but before
	// Central receives it. Historical reporting must retain the sale-time fact.
	if _, err := db.SQL().ExecContext(ctx, `UPDATE catalog_categories SET name=?, version=version+1 WHERE id=?`, "Snacks", "cat-beverages"); err != nil {
		t.Fatalf("recategorize current POS catalog: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	order.CompletedAt = &now
	order.UpdatedAt = now
	order.Version = 2
	eventID := "evt-reporting-category-e2e"
	payload := map[string]any{
		"order": order,
		"payments": []map[string]any{},
		"receipt": map[string]any{
			"id":              "receipt-reporting-category-e2e",
			"receipt_number":  "REPORT-CAT-1",
			"document_type":   "sale",
			"store_id":        "store-e2e",
			"terminal_id":     "terminal-e2e",
			"currency":        "INR",
			"total_minor":     order.TotalMinor,
			"paid_minor":      order.TotalMinor,
			"balance_minor":   0,
			"snapshot":        map[string]any{"order_id": order.ID},
			"snapshot_sha256": "reporting-category-e2e",
			"issued_at":       now,
		},
		"inventory_movements": []map[string]any{},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal completed sale: %v", err)
	}
	metadataJSON, err := json.Marshal(map[string]any{
		"source": "pos_service", "store_id": "store-e2e", "terminal_id": "terminal-e2e", "occurred_at": now,
	})
	if err != nil {
		t.Fatalf("marshal event metadata: %v", err)
	}

	if _, err := db.SQL().Exec(`INSERT INTO outbox_events(
		id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
		payload_json,metadata_json,status,attempt_count,available_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`,
		eventID, "sales_order", order.ID, order.Version, "sale.completed", 1, "sales_order:"+order.ID,
		string(payloadJSON), string(metadataJSON), now, now); err != nil {
		_ = db.Close()
		t.Fatalf("enqueue completed sale: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close POS before restart: %v", err)
	}
	db, err = database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen POS after restart: %v", err)
	}
	defer func() { _ = db.Close() }()

	service := outbox.New(db)
	engine, err := New(service, centralURL, tenantID, syncToken, deviceID, 5*time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected restarted POS category sale to dispatch to Central")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")

	// Lost-ack replay must preserve the same immutable Central sale line.
	if _, err := db.SQL().Exec(`UPDATE outbox_events
		SET status='pending', published_at=NULL, locked_at=NULL, last_error=NULL, available_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339Nano), eventID); err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected duplicate category sale replay to be accepted")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")
}
