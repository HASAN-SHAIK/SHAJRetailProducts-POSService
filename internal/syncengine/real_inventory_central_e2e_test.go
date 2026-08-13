package syncengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

func TestRealCentralInventoryConvergenceE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" {
		t.Skip("POS_E2E_CENTRAL_URL is not set")
	}

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos-real-inventory-e2e.db")
	db, err := database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open POS database: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("migrate POS database: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().Exec(`INSERT INTO catalog_products(id,name,track_inventory,updated_at) VALUES('101','Milk',1,?)`, now); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.SQL().Exec(`INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at) VALUES('store-e2e','101',5000,0,1,?)`, now); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}

	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	order := orders.Order{
		ID: "ord-inventory-e2e",
		StoreID: "store-e2e",
		Items: []orders.Item{{
			ID: "itm-inventory-e2e",
			LineNo: 1,
			ProductID: "101",
			ProductName: "Milk",
			QuantityMilli: 1000,
		}},
	}
	if err := inventory.New(db).ApplySaleTx(ctx, tx, order); err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		t.Fatalf("apply local sale inventory: %v", err)
	}
	if err := tx.Commit(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}

	var eventID string
	if err := db.SQL().QueryRow(`SELECT id FROM outbox_events WHERE event_type='inventory.movement.recorded' LIMIT 1`).Scan(&eventID); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	var localOnHand int64
	if err := db.SQL().QueryRow(`SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-e2e' AND product_id='101'`).Scan(&localOnHand); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if localOnHand != 4000 {
		_ = db.Close()
		t.Fatalf("local on_hand_milli=%d want=4000", localOnHand)
	}

	// The SQLite balance, immutable movement, and durable outbox survive an
	// offline process restart before Central becomes reachable.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen POS database: %v", err)
	}
	defer func() { _ = db.Close() }()
	service := outbox.New(db)
	assertRealCentralOutboxState(t, db, eventID, "pending")

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
		t.Fatal("expected reconnect dispatch to publish inventory movement")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")

	// Simulate a lost acknowledgement/retry of the exact same durable event.
	// Central must not apply the stock delta twice and POS must converge back to
	// published when Central reports that the event was already received.
	if _, err := db.SQL().Exec(`UPDATE outbox_events SET status='pending',published_at=NULL,locked_at=NULL,last_error=NULL,available_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), eventID); err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected replay dispatch to converge after duplicate delivery")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")
}
