package syncengine

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestRealCentralReceiptE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" {
		t.Skip("POS_E2E_CENTRAL_URL is not set")
	}

	tenantID := envOr("POS_E2E_TENANT_ID", "tenant-e2e")
	syncToken := envOr("POS_E2E_SYNC_TOKEN", "sync-secret")
	deviceID := envOr("POS_E2E_DEVICE_ID", "device-e2e")

	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	completedAt := now
	terminalID := "terminal-e2e"
	orderID := "ord-receipt-cross-repo-e2e"

	_, err := db.SQL().Exec(`INSERT INTO sales_orders(
		id,client_order_id,store_id,terminal_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,
		source,version,completed_at,created_at,updated_at)
		VALUES(?,?,?,?, 'confirmed','INR',12500,0,0,12500,'pos',2,?,?,?)`,
		orderID, "client-order-receipt-cross-repo-e2e", "store-e2e", terminalID, completedAt, now, now)
	if err != nil {
		t.Fatal(err)
	}

	order := orders.Order{
		ID:            orderID,
		ClientOrderID: "client-order-receipt-cross-repo-e2e",
		StoreID:       "store-e2e",
		TerminalID:    &terminalID,
		Status:        "confirmed",
		Currency:      "INR",
		SubtotalMinor: 12500,
		TotalMinor:    12500,
		Version:       2,
		CompletedAt:   &completedAt,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return receipts.New(db).ApplyCompletionTx(ctx, tx, order)
	}); err != nil {
		t.Fatal(err)
	}

	receipt, err := receipts.New(db).GetByOrder(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ID == "" || receipt.ReceiptNumber == "" || receipt.SnapshotSHA256 == "" {
		t.Fatalf("receipt missing immutable identity fields: %#v", receipt)
	}

	var eventID string
	if err := db.SQL().QueryRow(`SELECT id FROM outbox_events WHERE aggregate_type='receipt' AND aggregate_id=? AND event_type='receipt.issued'`, receipt.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	// Keep the final sale summary pending so this test proves the receipt can be
	// projected centrally before the completed sale projection exists.
	_, err = db.SQL().Exec(`UPDATE outbox_events SET available_at=? WHERE event_type='sale.completed' AND aggregate_id=?`,
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), orderID)
	if err != nil {
		t.Fatal(err)
	}

	engine, err := New(outbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected receipt dispatch to process the outbox event")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")

	// Re-read the immutable local receipt to prove reprint/read semantics return
	// the same stored document rather than a regenerated receipt.
	reprint, err := receipts.New(db).Get(ctx, receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reprint.ReceiptNumber != receipt.ReceiptNumber || reprint.SnapshotSHA256 != receipt.SnapshotSHA256 {
		t.Fatalf("receipt changed on re-read: first=%#v reprint=%#v", receipt, reprint)
	}

	// Replay the exact same durable receipt event. Central should acknowledge it
	// idempotently without creating another receipt projection.
	_, err = db.SQL().Exec(`UPDATE outbox_events
		SET status='pending', published_at=NULL, locked_at=NULL,
			last_error=NULL, available_at=?
		WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), eventID)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected receipt replay dispatch to process the outbox event")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")
}
