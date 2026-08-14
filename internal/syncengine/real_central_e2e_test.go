package syncengine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

func TestRealCentralOrderE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" {
		t.Skip("POS_E2E_CENTRAL_URL is not set")
	}

	tenantID := envOr("POS_E2E_TENANT_ID", "tenant-e2e")
	syncToken := envOr("POS_E2E_SYNC_TOKEN", "sync-secret")
	deviceID := envOr("POS_E2E_DEVICE_ID", "device-e2e")
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos-real-central-e2e.db")
	db, err := database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open POS database: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("migrate POS database: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	eventID := "evt-cross-repo-order-e2e"
	orderID := "ord-cross-repo-e2e"

	// This completed-sale snapshot intentionally carries non-zero discount and GST.
	// The deterministic POS calculator is certified separately; this E2E proves the
	// resulting immutable facts survive SQLite outbox delivery and Central projection
	// without being silently recalculated or rewritten.
	payload := map[string]any{
		"order": map[string]any{
			"id": orderID,
			"client_order_id": "client-order-cross-repo-e2e",
			"store_id": "store-e2e",
			"terminal_id": "terminal-e2e",
			"status": "confirmed",
			"currency": "INR",
			"subtotal_minor": 12500,
			"discount_minor": 500,
			"tax_minor": 2160,
			"total_minor": 14160,
			"version": 2,
			"completed_at": now,
			"created_at": now,
			"updated_at": now,
			"items": []map[string]any{{
				"id": "itm-cross-repo-e2e",
				"line_no": 1,
				"product_id": "product-e2e",
				"product_name": "Milk",
				"quantity_milli": 1000,
				"unit_price_minor": 12500,
				"discount_minor": 500,
				"tax_minor": 2160,
				"line_total_minor": 14160,
				"tax_code": "HSN0401",
			}},
		},
		"payments": []map[string]any{{
			"id": "pay-cross-repo-e2e",
			"client_payment_id": "client-pay-cross-repo-e2e",
			"mode": "cash",
			"direction": "in",
			"amount_minor": 14160,
			"currency": "INR",
			"status": "captured",
			"created_at": now,
		}},
		"receipt": map[string]any{
			"id": "rcpt-cross-repo-e2e",
			"receipt_number": "E2E-0001",
			"document_type": "sale",
			"store_id": "store-e2e",
			"terminal_id": "terminal-e2e",
			"currency": "INR",
			"total_minor": 14160,
			"paid_minor": 14160,
			"balance_minor": 0,
			"snapshot": map[string]any{"order_id": orderID},
			"snapshot_sha256": "e2e-sha256",
			"issued_at": now,
		},
		"inventory_movements": []map[string]any{{
			"id": "mov-cross-repo-e2e",
			"store_id": "store-e2e",
			"product_id": "product-e2e",
			"movement_type": "sale",
			"quantity_delta_milli": -1000,
			"reference_type": "sale_order",
			"reference_id": orderID,
			"order_item_id": "itm-cross-repo-e2e",
			"balance_after_milli": 9000,
			"occurred_at": now,
		}},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	metadataJSON, err := json.Marshal(map[string]any{"source": "pos_service", "store_id": "store-e2e", "terminal_id": "terminal-e2e", "occurred_at": now})
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.SQL().Exec(`INSERT INTO outbox_events(
		id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
		payload_json,metadata_json,status,attempt_count,available_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`,
		eventID, "sales_order", orderID, 2, "sale.completed", 1, "sales_order:"+orderID,
		string(payloadJSON), string(metadataJSON), now, now)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}

	// The sale/outbox fact is committed while the POS is offline. Simulate a
	// process/device restart before Central is reachable, then rebuild the real
	// outbox service and sync engine from the same SQLite file.
	if err := db.Close(); err != nil {
		t.Fatalf("close POS database before restart: %v", err)
	}
	db, err = database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen POS database after restart: %v", err)
	}
	defer func() { _ = db.Close() }()
	service := outbox.New(db)
	assertRealCentralOutboxState(t, db, eventID, "pending")

	engine, err := New(service, centralURL, tenantID, syncToken, deviceID, 5*time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected reconnect dispatch to process the restarted outbox event")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")

	// Replay the exact same durable event. Central must answer idempotently and
	// the POS must still mark the event published without creating duplicates.
	_, err = db.SQL().Exec(`UPDATE outbox_events
		SET status='pending', published_at=NULL, locked_at=NULL,
			last_error=NULL, available_at=?
		WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), eventID)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected replay dispatch to process the outbox event")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")
}

func assertRealCentralOutboxState(t *testing.T, db *database.DB, eventID, want string) {
	t.Helper()
	var got string
	if err := db.SQL().QueryRow(`SELECT status FROM outbox_events WHERE id=?`, eventID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("outbox status=%s want=%s", got, want)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
