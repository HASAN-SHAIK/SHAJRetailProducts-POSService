package syncengine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/refunds"
)

func openRealPartialReturnE2EDatabase(t *testing.T, path string) *database.DB {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatalf("open partial-return E2E database: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("migrate partial-return E2E database: %v", err)
	}
	return db
}

func TestRealCentralPartialReturnE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" {
		t.Skip("POS_E2E_CENTRAL_URL is not set")
	}

	tenantID := envOr("POS_E2E_TENANT_ID", "tenant-e2e")
	syncToken := envOr("POS_E2E_SYNC_TOKEN", "sync-secret")
	deviceID := envOr("POS_E2E_DEVICE_ID", "device-e2e")

	dbPath := filepath.Join(t.TempDir(), "pos-test.db")
	db := openRealPartialReturnE2EDatabase(t, dbPath)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	orderID := "ord-partial-return-cross-repo-e2e"
	itemID := "item-partial-return-cross-repo-e2e"
	returnID := "ret-partial-cross-repo-e2e"
	managerID := "manager-partial-return-e2e"
	reason := "customer returned one quarter"

	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES('102','Partial Return E2E Product','unit',1,0,1,1,?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO sales_orders(id,client_order_id,store_id,terminal_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,source,version,completed_at,created_at,updated_at) VALUES(?,?,?,?,'paid','INR',10000,0,0,10000,'pos',2,?,?,?)`, orderID, "client-partial-return-cross-repo-e2e", "store-e2e", "terminal-e2e", now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO sales_order_items(id,order_id,line_no,product_id,product_name,quantity_milli,unit_price_minor,discount_minor,tax_minor,line_total_minor,created_at) VALUES(?,?,1,'102','Partial Return E2E Product',1000,10000,0,0,10000,?)`, itemID, orderID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at) VALUES('store-e2e','102',4000,0,2,?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_movements(id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at) VALUES('issue-partial-return-cross-repo-e2e','store-e2e','102','sale_issue',-1000,'sale_order',?,?,4000,?,?)`, orderID, itemID, now, now); err != nil {
		t.Fatal(err)
	}

	paymentService := payments.New(db)
	capture, _, err := paymentService.Create(ctx, orderID, payments.CreateInput{ClientPaymentID: "capture-partial-return-cross-repo-e2e", Mode: "cash", AmountMinor: 10000, Currency: "INR", Status: "captured"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET status='published',published_at=? WHERE aggregate_type='payment' AND aggregate_id=?`, now, capture.ID); err != nil {
		t.Fatal(err)
	}

	completedPayload := map[string]any{
		"order": map[string]any{
			"id": orderID, "client_order_id": "client-partial-return-cross-repo-e2e", "store_id": "store-e2e", "terminal_id": "terminal-e2e",
			"status": "paid", "currency": "INR", "subtotal_minor": 10000, "discount_minor": 0, "tax_minor": 0, "total_minor": 10000,
			"version": 2, "completed_at": now, "created_at": now, "updated_at": now,
			"items": []map[string]any{{"id": itemID, "line_no": 1, "product_id": "102", "product_name": "Partial Return E2E Product", "quantity_milli": 1000, "unit_price_minor": 10000, "discount_minor": 0, "tax_minor": 0, "line_total_minor": 10000}},
		},
		"payments": []map[string]any{{"id": capture.ID, "client_payment_id": capture.ClientPaymentID, "mode": capture.Mode, "direction": "in", "amount_minor": 10000, "currency": "INR", "status": "captured", "created_at": now}},
		"receipt": map[string]any{
			"id": "receipt-partial-return-cross-repo-e2e", "receipt_number": "PARTIAL-RETURN-E2E-0001", "document_type": "sale", "store_id": "store-e2e", "terminal_id": "terminal-e2e",
			"currency": "INR", "total_minor": 10000, "paid_minor": 10000, "balance_minor": 0,
			"snapshot": map[string]any{"order_id": orderID}, "snapshot_sha256": "partial-return-e2e-snapshot-sha", "issued_at": now,
		},
		"inventory_movements": []map[string]any{{"id": "issue-partial-return-cross-repo-e2e", "store_id": "store-e2e", "product_id": "102", "movement_type": "sale_issue", "quantity_delta_milli": -1000, "reference_type": "sale_order", "reference_id": orderID, "order_item_id": itemID, "balance_after_milli": 4000, "occurred_at": now}},
	}
	payloadJSON, err := json.Marshal(completedPayload)
	if err != nil {
		t.Fatal(err)
	}
	metadataJSON, err := json.Marshal(map[string]any{"source": "pos_service", "store_id": "store-e2e", "terminal_id": "terminal-e2e", "occurred_at": now})
	if err != nil {
		t.Fatal(err)
	}
	completedEventID := "evt-partial-return-cross-repo-sale-completed"
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`, completedEventID, "sales_order", orderID, 2, "sale.completed", 1, "sales_order:"+orderID, string(payloadJSON), string(metadataJSON), now, now); err != nil {
		t.Fatal(err)
	}

	engine, err := New(outbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected sale.completed dispatch")
	}
	assertRealCentralOutboxState(t, db, completedEventID, "published")

	refundService := refunds.New(db, orders.New(db, nil), paymentService, inventory.New(db))
	updated, plan, err := refundService.ReturnPartial(ctx, refunds.PartialReturnInput{
		ReturnID: returnID,
		OrderID: orderID,
		ApprovedByUserID: managerID,
		Reason: reason,
		Lines: []refunds.PartialReturnLineInput{{OrderItemID: itemID, QuantityMilli: 250}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status == "returned" || plan.FullRemaining || plan.RefundMinor != 2500 {
		t.Fatalf("unexpected partial return state status=%s full=%v refund=%d", updated.Status, plan.FullRemaining, plan.RefundMinor)
	}

	var outbound, saleReturns int
	var onHand int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id=? AND direction='out' AND status='refunded' AND amount_minor=2500`, orderID).Scan(&outbound); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id=? AND movement_type='sale_return' AND quantity_delta_milli=250`, itemID).Scan(&saleReturns); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-e2e' AND product_id='102'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if outbound != 1 || saleReturns != 1 || onHand != 4250 {
		t.Fatalf("local partial return compensation outbound=%d sale_returns=%d on_hand=%d", outbound, saleReturns, onHand)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openRealPartialReturnE2EDatabase(t, dbPath)

	var pendingAfterRestart int
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE status='pending' AND ordering_key=?
		  AND event_type IN ('sale.partial_returned','payment.recorded','inventory.movement.recorded')`, "sales_order:"+orderID).Scan(&pendingAfterRestart); err != nil {
		t.Fatal(err)
	}
	if pendingAfterRestart != 3 {
		t.Fatalf("pending partial-return facts after restart=%d want=3", pendingAfterRestart)
	}

	engine, err = New(outbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		var pending int
		if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE status='pending' AND available_at<=?`, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			break
		}
		if !engine.dispatchOne(ctx) {
			t.Fatal("partial return outbox remained pending after restart but dispatch made no progress")
		}
	}
	var pending int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE status='pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("partial return left %d pending outbox events after restart", pending)
	}

	var refundEventIDs []string
	rows, err := db.SQL().QueryContext(ctx, `
		SELECT id FROM outbox_events
		WHERE ordering_key=?
		  AND (
		    event_type IN ('inventory.movement.recorded','sale.partial_returned')
		    OR (
		      event_type='payment.recorded'
		      AND aggregate_id IN (SELECT id FROM payments WHERE order_id=? AND direction='out')
		    )
		  )
		ORDER BY created_at,id`, "sales_order:"+orderID, orderID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		refundEventIDs = append(refundEventIDs, id)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(refundEventIDs) != 3 {
		t.Fatalf("refund event chain count=%d want=3 ids=%v", len(refundEventIDs), refundEventIDs)
	}

	// Model lost acknowledgements after Central has already committed all three
	// refund facts: POS still believes each durable fact is pending and retries
	// the exact same event identity. Restart POS before replay so the executable
	// cross-repo path also proves durable recovery of those unacknowledged facts.
	for _, id := range refundEventIDs {
		if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET status='pending',published_at=NULL,locked_at=NULL,last_error=NULL,available_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openRealPartialReturnE2EDatabase(t, dbPath)
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE status='pending' AND ordering_key=? AND id IN (?,?,?)`, "sales_order:"+orderID, refundEventIDs[0], refundEventIDs[1], refundEventIDs[2]).Scan(&pendingAfterRestart); err != nil {
		t.Fatal(err)
	}
	if pendingAfterRestart != len(refundEventIDs) {
		t.Fatalf("lost-ack refund facts after restart=%d want=%d", pendingAfterRestart, len(refundEventIDs))
	}
	engine, err = New(outbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range refundEventIDs {
		if !engine.dispatchOne(ctx) {
			t.Fatalf("expected lost-ack refund replay dispatch %d for %s after restart", i+1, id)
		}
		assertRealCentralOutboxState(t, db, id, "published")
	}

	// Operator-visible reconciliation must converge after the strict Central
	// duplicate acknowledgements are accepted. Recreate the read-only service on
	// the reopened database to ensure the snapshot itself is restart-safe.
	reconciliationService := refunds.New(db, orders.New(db, nil), payments.New(db), inventory.New(db))
	snapshot, err := reconciliationService.GetReconciliationSnapshot(ctx, orderID)
	if err != nil {
		t.Fatalf("read reconciliation after lost-ack restart replay: %v", err)
	}
	if snapshot.UnpublishedSyncFacts != 0 || snapshot.DeadLetterSyncFacts != 0 || snapshot.DeadLetterSyncHead != nil {
		t.Fatalf("reconciliation did not converge after lost-ack restart replay: unpublished=%d dead_letter=%d head=%v", snapshot.UnpublishedSyncFacts, snapshot.DeadLetterSyncFacts, snapshot.DeadLetterSyncHead)
	}
	if snapshot.CapturedPaymentMinor != 10000 || snapshot.ReversedPaymentMinor != 2500 {
		t.Fatalf("unexpected reconciled payment facts captured=%d reversed=%d", snapshot.CapturedPaymentMinor, snapshot.ReversedPaymentMinor)
	}
	if snapshot.SaleIssuedQuantityMilli != 1000 || snapshot.RestoredQuantityMilli != 250 {
		t.Fatalf("unexpected reconciled inventory facts issued=%d restored=%d", snapshot.SaleIssuedQuantityMilli, snapshot.RestoredQuantityMilli)
	}
	if snapshot.PartialReturnOperations != 1 || snapshot.PartialReturnRefundMinor != 2500 {
		t.Fatalf("unexpected reconciled partial-return facts operations=%d refund=%d", snapshot.PartialReturnOperations, snapshot.PartialReturnRefundMinor)
	}
}
