package refunds

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
)

func TestGetReconciliationSnapshotSummarizesRefundFacts(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	svc, paymentService := newRefundService(db)

	if _, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{
		ClientPaymentID: "capture-reconciliation", Mode: "cash", AmountMinor: 10000, Currency: "INR", Status: "captured",
	}); err != nil {
		t.Fatal(err)
	}

	before, err := svc.GetReconciliationSnapshot(ctx, "ord-refund-full")
	if err != nil {
		t.Fatal(err)
	}
	if before.OrderStatus != "paid" || before.CapturedPaymentMinor != 10000 || before.ReversedPaymentMinor != 0 {
		t.Fatalf("before payment facts=%+v", before)
	}
	if before.SaleIssuedQuantityMilli != 1000 || before.RestoredQuantityMilli != 0 {
		t.Fatalf("before inventory facts=%+v", before)
	}

	if _, err := svc.RefundFullSale(ctx, "ord-refund-full", "manager-1", "customer return"); err != nil {
		t.Fatal(err)
	}

	after, err := svc.GetReconciliationSnapshot(ctx, "ord-refund-full")
	if err != nil {
		t.Fatal(err)
	}
	if after.OrderStatus != "returned" || after.CapturedPaymentMinor != 10000 || after.ReversedPaymentMinor != 10000 {
		t.Fatalf("after payment facts=%+v", after)
	}
	if after.SaleIssuedQuantityMilli != 1000 || after.RestoredQuantityMilli != 1000 {
		t.Fatalf("after inventory facts=%+v", after)
	}
	if after.PartialReturnOperations != 0 || after.PartialReturnRefundMinor != 0 {
		t.Fatalf("unexpected partial facts=%+v", after)
	}
}

func TestGetReconciliationSnapshotIncludesDurablePartialReturnTotals(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	svc, _ := newRefundService(db)

	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := AppendPartialReturnTx(ctx, tx, PartialReturnLedgerRecord{
			ID: "ret-reconciliation", OrderID: "ord-refund-full", ApprovedByUserID: "manager-1",
			Reason: "damaged", RefundMinor: 2500, CreatedAt: "2026-08-09T10:00:00Z",
			Lines: []PartialReturnLedgerLine{{OrderItemID: "item-refund-full", QuantityMilli: 250, RefundMinor: 2500}},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := svc.GetReconciliationSnapshot(ctx, "ord-refund-full")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PartialReturnOperations != 1 || snapshot.PartialReturnRefundMinor != 2500 {
		t.Fatalf("partial facts=%+v", snapshot)
	}
}

func TestGetReconciliationSnapshotRejectsInvalidOrMissingOrder(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	svc, _ := newRefundService(db)

	if _, err := svc.GetReconciliationSnapshot(ctx, " "); !errors.Is(err, ErrInvalidPartialReturn) {
		t.Fatalf("blank order error=%v", err)
	}
	if _, err := svc.GetReconciliationSnapshot(ctx, "missing-order"); !errors.Is(err, orders.ErrNotFound) {
		t.Fatalf("missing order error=%v", err)
	}
}
