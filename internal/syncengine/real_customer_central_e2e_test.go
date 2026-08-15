package syncengine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

func TestRealCentralCustomerCanonicalConvergenceE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" {
		t.Skip("POS_E2E_CENTRAL_URL is not set")
	}

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos-customer-central-e2e.db")
	db, err := database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open POS database: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("migrate POS database: %v", err)
	}

	phone := "90000 00000"
	email := "offline-customer@example.test"
	taxID := "GST-CUSTOMER-E2E"
	created, err := customer.NewRepository(db).Create(ctx, customer.UpsertInput{
		Name:             "Offline Customer E2E",
		Phone:            &phone,
		Email:            &email,
		TaxID:            &taxID,
		CreditLimitMinor: 990000,
		Currency:         "INR",
	})
	if err != nil {
		_ = db.Close()
		t.Fatalf("create offline customer: %v", err)
	}
	customerEventID := "evt_customer_" + created.ID + "_v1"
	assertRealCentralOutboxState(t, db, customerEventID, "pending")

	// The customer and durable event already exist while offline. Restart before
	// Central is reachable, then dispatch the exact persisted event.
	if err := db.Close(); err != nil {
		t.Fatalf("close POS database before restart: %v", err)
	}
	db, err = database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen POS database after restart: %v", err)
	}
	defer func() { _ = db.Close() }()
	service := outbox.New(db)
	engine, err := New(
		service,
		centralURL,
		envOr("POS_E2E_TENANT_ID", "tenant-e2e"),
		envOr("POS_E2E_SYNC_TOKEN", "sync-secret"),
		envOr("POS_E2E_DEVICE_ID", "device-e2e"),
		5*time.Second,
		50*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected customer reconnect dispatch")
	}
	assertRealCentralOutboxState(t, db, customerEventID, "published")

	// Lost acknowledgement / replay of the immutable customer fact must not create
	// another canonical customer or remap the POS identity.
	if _, err := db.SQL().Exec(`UPDATE outbox_events
		SET status='pending',published_at=NULL,locked_at=NULL,last_error=NULL,available_at=?
		WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), customerEventID); err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected customer replay dispatch")
	}
	assertRealCentralOutboxState(t, db, customerEventID, "published")

	// Transaction Core already certifies sale creation. This dependent Customers
	// acceptance publishes a valid immutable completed-sale snapshot that refers to
	// the offline POS customer ID, proving Central links it through the mapping.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	orderID := "ord-customer-link-e2e"
	saleEventID := "evt-customer-link-sale-e2e"
	payload := map[string]any{
		"order": map[string]any{
			"id": orderID,
			"client_order_id": "client-customer-link-e2e",
			"store_id": "store-e2e",
			"terminal_id": "terminal-e2e",
			"customer_id": created.ID,
			"status": "confirmed",
			"currency": "INR",
			"subtotal_minor": 10000,
			"discount_minor": 0,
			"tax_minor": 0,
			"total_minor": 10000,
			"version": 2,
			"completed_at": now,
			"created_at": now,
			"updated_at": now,
			"items": []map[string]any{{
				"id": "itm-customer-link-e2e",
				"line_no": 1,
				"product_id": "product-e2e",
				"product_name": "Milk",
				"quantity_milli": 1000,
				"unit_price_minor": 10000,
				"discount_minor": 0,
				"tax_minor": 0,
				"line_total_minor": 10000,
			}},
		},
		"payments": []map[string]any{{
			"id": "pay-customer-link-e2e",
			"client_payment_id": "client-pay-customer-link-e2e",
			"mode": "cash",
			"direction": "in",
			"amount_minor": 10000,
			"currency": "INR",
			"status": "captured",
			"created_at": now,
		}},
		"receipt": map[string]any{
			"id": "rcpt-customer-link-e2e",
			"receipt_number": "CUS-E2E-0001",
			"document_type": "sale",
			"store_id": "store-e2e",
			"terminal_id": "terminal-e2e",
			"customer_id": created.ID,
			"currency": "INR",
			"total_minor": 10000,
			"paid_minor": 10000,
			"balance_minor": 0,
			"snapshot": map[string]any{"order_id": orderID},
			"snapshot_sha256": "customer-e2e-sha256",
			"issued_at": now,
		},
		"inventory_movements": []map[string]any{},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	metadataJSON, _ := json.Marshal(map[string]any{"source": "pos_service", "store_id": "store-e2e", "terminal_id": "terminal-e2e", "occurred_at": now})
	if _, err := db.SQL().Exec(`INSERT INTO outbox_events(
		id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
		payload_json,metadata_json,status,attempt_count,available_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`,
		saleEventID, "sales_order", orderID, 2, "sale.completed", 1, "sales_order:"+orderID,
		string(payloadJSON), string(metadataJSON), now, now); err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected customer-linked sale dispatch")
	}
	assertRealCentralOutboxState(t, db, saleEventID, "published")

	// Export the random POS-local identity for PostgreSQL assertions in the workflow.
	if path := os.Getenv("POS_E2E_CUSTOMER_ID_FILE"); path != "" {
		if err := os.WriteFile(path, []byte(created.ID), 0o600); err != nil {
			t.Fatalf("write customer id file: %v", err)
		}
	}
}
