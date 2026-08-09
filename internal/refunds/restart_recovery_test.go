package refunds

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
)

func openRestartRefundDB(t *testing.T, path string) *database.DB {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return db
}

func TestPartialRefundFactsSurvivePOSRestartAndReplay(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")
	db := openRestartRefundDB(t, dbPath)
	seedCompletedSale(t, db, true)
	svc, paymentService := newRefundService(db)

	capture, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{
		ClientPaymentID: "capture-restart", Mode: "cash", AmountMinor: 10000, Currency: "INR", Status: "captured",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Isolate the durable facts produced by the return itself. The original
	// capture is assumed already acknowledged before the network interruption.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
		UPDATE outbox_events SET status='published',published_at=?
		WHERE aggregate_type='payment' AND aggregate_id=?`, now, capture.ID); err != nil {
		t.Fatal(err)
	}

	input := PartialReturnInput{
		ReturnID: "ret-restart-1", OrderID: "ord-refund-full", ApprovedByUserID: "manager-restart",
		Reason: "restart durability", Lines: []PartialReturnLineInput{{OrderItemID: "item-refund-full", QuantityMilli: 250}},
	}
	order, plan, err := svc.ReturnPartial(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status == "returned" || plan.FullRemaining || plan.RefundMinor != 2500 {
		t.Fatalf("unexpected pre-restart result order=%+v plan=%+v", order, plan)
	}

	var pendingBefore int
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE status='pending' AND ordering_key='sales_order:ord-refund-full'
		  AND event_type IN ('sale.partial_returned','payment.recorded','inventory.movement.recorded')`).Scan(&pendingBefore); err != nil {
		t.Fatal(err)
	}
	if pendingBefore != 3 {
		t.Fatalf("pending refund facts before restart=%d want=3", pendingBefore)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate the POS process restarting and opening the same on-disk SQLite DB.
	db = openRestartRefundDB(t, dbPath)
	defer db.Close()
	svc, _ = newRefundService(db)

	history, err := svc.ListPartialReturns(ctx, "ord-refund-full")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ID != input.ReturnID || history[0].ApprovedByUserID != input.ApprovedByUserID || history[0].Reason != input.Reason || history[0].RefundMinor != 2500 {
		t.Fatalf("durable return history after restart=%+v", history)
	}
	if len(history[0].Lines) != 1 || history[0].Lines[0].OrderItemID != "item-refund-full" || history[0].Lines[0].QuantityMilli != 250 || history[0].Lines[0].RefundMinor != 2500 {
		t.Fatalf("durable return lines after restart=%+v", history[0].Lines)
	}

	snapshot, err := svc.GetReconciliationSnapshot(ctx, "ord-refund-full")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CapturedPaymentMinor != 10000 || snapshot.ReversedPaymentMinor != 2500 || snapshot.SaleIssuedQuantityMilli != 1000 || snapshot.RestoredQuantityMilli != 250 || snapshot.PartialReturnOperations != 1 || snapshot.PartialReturnRefundMinor != 2500 {
		t.Fatalf("reconciliation facts after restart=%+v", snapshot)
	}

	var pendingAfter int
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE status='pending' AND ordering_key='sales_order:ord-refund-full'
		  AND event_type IN ('sale.partial_returned','payment.recorded','inventory.movement.recorded')`).Scan(&pendingAfter); err != nil {
		t.Fatal(err)
	}
	if pendingAfter != pendingBefore {
		t.Fatalf("restart changed pending refund facts before=%d after=%d", pendingBefore, pendingAfter)
	}

	// An application-level retry after restart must reconstruct the same durable
	// operation and must not duplicate money, inventory, ledger, or outbox facts.
	replayedOrder, replayedPlan, err := svc.ReturnPartial(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if replayedPlan.RefundMinor != 2500 || replayedOrder.ID != "ord-refund-full" {
		t.Fatalf("unexpected replay result order=%+v plan=%+v", replayedOrder, replayedPlan)
	}

	var ledgerCount, outboundCount, movementCount, partialEventCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_partial_returns WHERE id=?`, input.ReturnID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id='ord-refund-full' AND direction='out' AND status='refunded'`).Scan(&outboundCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='item-refund-full' AND movement_type='sale_return'`).Scan(&movementCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-full' AND event_type='sale.partial_returned'`).Scan(&partialEventCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 || outboundCount != 1 || movementCount != 1 || partialEventCount != 1 {
		t.Fatalf("restart replay duplicated facts ledger=%d outbound=%d movements=%d partial_events=%d", ledgerCount, outboundCount, movementCount, partialEventCount)
	}
}
