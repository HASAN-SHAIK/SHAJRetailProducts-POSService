package syncengine

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/refunds"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestRealCentralRefundE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" { t.Skip("POS_E2E_CENTRAL_URL is not set") }

	tenantID := envOr("POS_E2E_TENANT_ID", "tenant-e2e")
	syncToken := envOr("POS_E2E_SYNC_TOKEN", "sync-secret")
	deviceID := envOr("POS_E2E_DEVICE_ID", "device-e2e")

	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	orderID := "ord-refund-cross-repo-e2e"
	itemID := "item-refund-cross-repo-e2e"
	managerID := "manager-refund-e2e"
	reason := "customer returned full sale"

	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES('product-refund-e2e','Refund E2E Product','unit',1,0,1,1,?)`, now); err != nil { t.Fatal(err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO sales_orders(id,client_order_id,store_id,terminal_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,source,version,completed_at,created_at,updated_at) VALUES(?,?,?,?,'paid','INR',10000,0,0,10000,'pos',2,?,?,?)`, orderID, "client-refund-cross-repo-e2e", "store-e2e", "terminal-e2e", now, now, now); err != nil { t.Fatal(err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO sales_order_items(id,order_id,line_no,product_id,product_name,quantity_milli,unit_price_minor,discount_minor,tax_minor,line_total_minor,created_at) VALUES(?,?,1,'product-refund-e2e','Refund E2E Product',1000,10000,0,0,10000,?)`, itemID, orderID, now); err != nil { t.Fatal(err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at) VALUES('store-e2e','product-refund-e2e',4000,0,2,?)`, now); err != nil { t.Fatal(err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_movements(id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at) VALUES('issue-refund-cross-repo-e2e','store-e2e','product-refund-e2e','sale_issue',-1000,'sale_order',?,?,4000,?,?)`, orderID, itemID, now, now); err != nil { t.Fatal(err) }

	paymentService := payments.New(db)
	capture, _, err := paymentService.Create(ctx, orderID, payments.CreateInput{ClientPaymentID: "capture-refund-cross-repo-e2e", Mode: "cash", AmountMinor: 10000, Currency: "INR", Status: "captured"})
	if err != nil { t.Fatal(err) }
	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET status='published',published_at=? WHERE aggregate_type='payment' AND aggregate_id=?`, now, capture.ID); err != nil { t.Fatal(err) }

	completedPayload := map[string]any{
		"order": map[string]any{
			"id": orderID, "client_order_id": "client-refund-cross-repo-e2e", "store_id": "store-e2e", "terminal_id": "terminal-e2e",
			"status": "paid", "currency": "INR", "subtotal_minor": 10000, "discount_minor": 0, "tax_minor": 0, "total_minor": 10000,
			"version": 2, "completed_at": now, "created_at": now, "updated_at": now,
			"items": []map[string]any{{"id": itemID, "line_no": 1, "product_id": "product-refund-e2e", "product_name": "Refund E2E Product", "quantity_milli": 1000, "unit_price_minor": 10000, "discount_minor": 0, "tax_minor": 0, "line_total_minor": 10000}},
		},
		"payments": []map[string]any{{"id": capture.ID, "client_payment_id": capture.ClientPaymentID, "mode": capture.Mode, "direction": "in", "amount_minor": 10000, "currency": "INR", "status": "captured", "created_at": now}},
		"receipt": map[string]any{
			"id": "receipt-refund-cross-repo-e2e", "receipt_number": "REFUND-E2E-0001", "document_type": "sale", "store_id": "store-e2e", "terminal_id": "terminal-e2e",
			"currency": "INR", "total_minor": 10000, "paid_minor": 10000, "balance_minor": 0,
			"snapshot": map[string]any{"order_id": orderID}, "snapshot_sha256": "refund-e2e-snapshot-sha", "issued_at": now,
		},
		"inventory_movements": []map[string]any{{"id": "issue-refund-cross-repo-e2e", "store_id": "store-e2e", "product_id": "product-refund-e2e", "movement_type": "sale_issue", "quantity_delta_milli": -1000, "reference_type": "sale_order", "reference_id": orderID, "order_item_id": itemID, "balance_after_milli": 4000, "occurred_at": now}},
	}
	payloadJSON, err := json.Marshal(completedPayload)
	if err != nil { t.Fatal(err) }
	metadataJSON, err := json.Marshal(map[string]any{"source": "pos_service", "store_id": "store-e2e", "terminal_id": "terminal-e2e", "occurred_at": now})
	if err != nil { t.Fatal(err) }
	completedEventID := "evt-refund-cross-repo-sale-completed"
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`, completedEventID, "sales_order", orderID, 2, "sale.completed", 1, "sales_order:"+orderID, string(payloadJSON), string(metadataJSON), now, now); err != nil { t.Fatal(err) }

	engine, err := New(outbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, 50*time.Millisecond)
	if err != nil { t.Fatal(err) }
	if !engine.dispatchOne(ctx) { t.Fatal("expected sale.completed dispatch") }
	assertRealCentralOutboxState(t, db, completedEventID, "published")

	refundService := refunds.New(db, orders.New(db, nil), paymentService, inventory.New(db))
	returned, err := refundService.RefundFullSale(ctx, orderID, managerID, reason)
	if err != nil { t.Fatal(err) }
	if returned.Status != "returned" || returned.Version != 3 { t.Fatalf("returned order status/version=%s/%d", returned.Status, returned.Version) }

	var outbound, saleReturns int
	var onHand int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id=? AND direction='out' AND status='refunded' AND amount_minor=10000`, orderID).Scan(&outbound); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id=? AND movement_type='sale_return'`, itemID).Scan(&saleReturns); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-e2e' AND product_id='product-refund-e2e'`).Scan(&onHand); err != nil { t.Fatal(err) }
	if outbound != 1 || saleReturns != 1 || onHand != 5000 { t.Fatalf("local refund compensation outbound=%d sale_returns=%d on_hand=%d", outbound, saleReturns, onHand) }

	for i := 0; i < 10; i++ {
		var pending int
		if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE status='pending' AND available_at<=?`, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&pending); err != nil { t.Fatal(err) }
		if pending == 0 { break }
		if !engine.dispatchOne(ctx) { t.Fatal("refund outbox remained pending but dispatch made no progress") }
	}
	var pending int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE status='pending'`).Scan(&pending); err != nil { t.Fatal(err) }
	if pending != 0 { t.Fatalf("refund left %d pending outbox events", pending) }

	var returnedEventID string
	if err := db.SQL().QueryRowContext(ctx, `SELECT id FROM outbox_events WHERE aggregate_id=? AND event_type='sale.returned'`, orderID).Scan(&returnedEventID); err != nil { t.Fatal(err) }
	assertRealCentralOutboxState(t, db, returnedEventID, "published")

	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET status='pending',published_at=NULL,locked_at=NULL,last_error=NULL,available_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), returnedEventID); err != nil { t.Fatal(err) }
	if !engine.dispatchOne(ctx) { t.Fatal("expected sale.returned replay dispatch") }
	assertRealCentralOutboxState(t, db, returnedEventID, "published")
}
