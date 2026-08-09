package refunds

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
)

func TestReturnPartialCommitsLedgerProportionalTenderAndInventoryAtomically(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	svc, paymentService := newRefundService(db)
	if _, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{
		ClientPaymentID: "capture-cash", Mode: "cash", AmountMinor: 6000, Currency: "INR", Status: "captured",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{
		ClientPaymentID: "capture-card", Mode: "card", AmountMinor: 4000, Currency: "INR", Status: "captured",
	}); err != nil {
		t.Fatal(err)
	}

	order, plan, err := svc.ReturnPartial(ctx, PartialReturnInput{
		ReturnID: "ret-partial-1", OrderID: "ord-refund-full", ApprovedByUserID: "manager-1", Reason: "customer returned one quarter",
		Lines: []PartialReturnLineInput{{OrderItemID: "item-refund-full", QuantityMilli: 250}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.RefundMinor != 2500 || plan.FullRemaining {
		t.Fatalf("plan=%+v", plan)
	}
	if order.Status == "returned" {
		t.Fatal("partial return must not finalize the sale")
	}

	var ledgerCount, returnMovements, returnedEvents int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_partial_returns WHERE id='ret-partial-1' AND refund_minor=2500`).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='item-refund-full' AND movement_type='sale_return'`).Scan(&returnMovements); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-full' AND event_type='sale.returned'`).Scan(&returnedEvents); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 || returnMovements != 1 || returnedEvents != 0 {
		t.Fatalf("ledger=%d movements=%d returnedEvents=%d", ledgerCount, returnMovements, returnedEvents)
	}

	var cashRefund, cardRefund int64
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(r.amount_minor),0)
		FROM payments r JOIN payments p ON r.reference=p.id
		WHERE r.order_id='ord-refund-full' AND r.direction='out' AND p.client_payment_id='capture-cash'`).Scan(&cashRefund); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(r.amount_minor),0)
		FROM payments r JOIN payments p ON r.reference=p.id
		WHERE r.order_id='ord-refund-full' AND r.direction='out' AND p.client_payment_id='capture-card'`).Scan(&cardRefund); err != nil {
		t.Fatal(err)
	}
	if cashRefund != 1500 || cardRefund != 1000 {
		t.Fatalf("cashRefund=%d cardRefund=%d", cashRefund, cardRefund)
	}

	var onHand int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 4250 {
		t.Fatalf("onHand=%d want=4250", onHand)
	}
}

func TestReturnPartialFinalRemainderConvergesExactlyAndReplayIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	svc, paymentService := newRefundService(db)
	if _, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{
		ClientPaymentID: "capture-cash", Mode: "cash", AmountMinor: 6000, Currency: "INR", Status: "captured",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{
		ClientPaymentID: "capture-card", Mode: "card", AmountMinor: 4000, Currency: "INR", Status: "captured",
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.ReturnPartial(ctx, PartialReturnInput{
		ReturnID: "ret-partial-1", OrderID: "ord-refund-full", ApprovedByUserID: "manager-1", Reason: "first part",
		Lines: []PartialReturnLineInput{{OrderItemID: "item-refund-full", QuantityMilli: 250}},
	}); err != nil {
		t.Fatal(err)
	}
	finalInput := PartialReturnInput{
		ReturnID: "ret-partial-2", OrderID: "ord-refund-full", ApprovedByUserID: "manager-2", Reason: "remaining goods",
		Lines: []PartialReturnLineInput{{OrderItemID: "item-refund-full", QuantityMilli: 750}},
	}
	order, plan, err := svc.ReturnPartial(ctx, finalInput)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.FullRemaining || plan.RefundMinor != 7500 || order.Status != "returned" {
		t.Fatalf("order=%+v plan=%+v", order, plan)
	}

	var totalRefund, totalReturnedQty int64
	var returnedEvents int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_minor),0) FROM payments WHERE order_id='ord-refund-full' AND direction='out'`).Scan(&totalRefund); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity_delta_milli),0) FROM inventory_movements WHERE order_item_id='item-refund-full' AND movement_type='sale_return'`).Scan(&totalReturnedQty); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-full' AND event_type='sale.returned'`).Scan(&returnedEvents); err != nil {
		t.Fatal(err)
	}
	if totalRefund != 10000 || totalReturnedQty != 1000 || returnedEvents != 1 {
		t.Fatalf("refund=%d qty=%d returnedEvents=%d", totalRefund, totalReturnedQty, returnedEvents)
	}

	if _, _, err := svc.ReturnPartial(ctx, finalInput); err != nil {
		t.Fatal(err)
	}
	var refundsAfterReplay, movementsAfterReplay, ledgerAfterReplay int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id='ord-refund-full' AND direction='out'`).Scan(&refundsAfterReplay); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='item-refund-full' AND movement_type='sale_return'`).Scan(&movementsAfterReplay); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_partial_returns WHERE order_id='ord-refund-full'`).Scan(&ledgerAfterReplay); err != nil {
		t.Fatal(err)
	}
	if refundsAfterReplay != 4 || movementsAfterReplay != 2 || ledgerAfterReplay != 2 {
		t.Fatalf("replay leaked refunds=%d movements=%d ledger=%d", refundsAfterReplay, movementsAfterReplay, ledgerAfterReplay)
	}
}

func TestReturnPartialRollsBackLedgerAndMoneyWhenInventoryFails(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, false)
	svc, paymentService := newRefundService(db)
	if _, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{
		ClientPaymentID: "capture-1", Mode: "cash", AmountMinor: 10000, Currency: "INR", Status: "captured",
	}); err != nil {
		t.Fatal(err)
	}

	_, _, err := svc.ReturnPartial(ctx, PartialReturnInput{
		ReturnID: "ret-rollback", OrderID: "ord-refund-full", ApprovedByUserID: "manager-1", Reason: "rollback test",
		Lines: []PartialReturnLineInput{{OrderItemID: "item-refund-full", QuantityMilli: 250}},
	})
	if err == nil {
		t.Fatal("expected inventory failure")
	}
	var ledger, outbound int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_partial_returns WHERE id='ret-rollback'`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id='ord-refund-full' AND direction='out'`).Scan(&outbound); err != nil {
		t.Fatal(err)
	}
	if ledger != 0 || outbound != 0 {
		t.Fatalf("rollback leaked ledger=%d outbound=%d", ledger, outbound)
	}
}
