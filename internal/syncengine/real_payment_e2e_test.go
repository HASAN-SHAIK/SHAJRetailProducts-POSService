package syncengine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestRealCentralPaymentE2E(t *testing.T) {
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
	orderID := "ord-payment-cross-repo-e2e"
	clientPaymentID := "client-pay-cross-repo-e2e"

	_, err := db.SQL().Exec(`INSERT INTO sales_orders(
		id,client_order_id,store_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,
		source,version,created_at,updated_at)
		VALUES(?,?,?,'confirmed','INR',?,0,0,?,'pos',1,?,?)`,
		orderID, "client-order-payment-cross-repo-e2e", "store-e2e", 12500, 12500, now, now)
	if err != nil {
		t.Fatal(err)
	}

	payment, summary, err := payments.New(db).Create(ctx, orderID, payments.CreateInput{
		ClientPaymentID: clientPaymentID,
		Mode:            "upi",
		AmountMinor:     5000,
		Currency:        "INR",
		Status:          "captured",
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.OrderStatus != "partially_paid" || summary.PaidMinor != 5000 || summary.BalanceMinor != 7500 {
		t.Fatalf("unexpected payment summary: %#v", summary)
	}

	var eventID string
	if err := db.SQL().QueryRow(`SELECT id FROM outbox_events WHERE aggregate_type='payment' AND aggregate_id=? AND event_type='payment.recorded'`, payment.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}

	engine, err := New(outbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected first payment dispatch to process the outbox event")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")

	// Replay the exact same durable payment event. Central must acknowledge it
	// idempotently and must not create a second central payment projection.
	_, err = db.SQL().Exec(`UPDATE outbox_events
		SET status='pending', published_at=NULL, locked_at=NULL,
			last_error=NULL, available_at=?
		WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), eventID)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected payment replay dispatch to process the outbox event")
	}
	assertRealCentralOutboxState(t, db, eventID, "published")
}
