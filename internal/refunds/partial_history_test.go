package refunds

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func TestListPartialReturnsReturnsDeterministicDurableOperations(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	svc, _ := newRefundService(db)

	first := PartialReturnLedgerRecord{
		ID: "ret-history-1", OrderID: "ord-refund-full", ApprovedByUserID: "manager-1",
		Reason: "damaged one", RefundMinor: 2500, CreatedAt: "2026-08-09T10:00:00Z",
		Lines: []PartialReturnLedgerLine{{OrderItemID: "item-refund-full", QuantityMilli: 250, RefundMinor: 2500}},
	}
	second := PartialReturnLedgerRecord{
		ID: "ret-history-2", OrderID: "ord-refund-full", ApprovedByUserID: "manager-2",
		Reason: "returned more", RefundMinor: 5000, CreatedAt: "2026-08-09T11:00:00Z",
		Lines: []PartialReturnLedgerLine{{OrderItemID: "item-refund-full", QuantityMilli: 500, RefundMinor: 5000}},
	}
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := AppendPartialReturnTx(ctx, tx, second); err != nil { return err }
		if _, err := AppendPartialReturnTx(ctx, tx, first); err != nil { return err }
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	records, err := svc.ListPartialReturns(ctx, "ord-refund-full")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records=%d", len(records))
	}
	if records[0].ID != first.ID || records[1].ID != second.ID {
		t.Fatalf("unexpected order: %+v", records)
	}
	if records[0].ApprovedByUserID != "manager-1" || records[0].Reason != "damaged one" || records[0].RefundMinor != 2500 {
		t.Fatalf("first operation audit mismatch: %+v", records[0])
	}
	if len(records[0].Lines) != 1 || records[0].Lines[0].OrderItemID != "item-refund-full" || records[0].Lines[0].QuantityMilli != 250 || records[0].Lines[0].RefundMinor != 2500 {
		t.Fatalf("first operation lines mismatch: %+v", records[0].Lines)
	}
}

func TestListPartialReturnsReturnsEmptyForOrderWithoutHistory(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	svc, _ := newRefundService(db)

	records, err := svc.ListPartialReturns(ctx, "ord-refund-full")
	if err != nil {
		t.Fatal(err)
	}
	if records == nil || len(records) != 0 {
		t.Fatalf("records=%+v", records)
	}
}

func TestListPartialReturnsRejectsUnknownOrInvalidOrder(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	svc, _ := newRefundService(db)

	if _, err := svc.ListPartialReturns(ctx, " "); !errors.Is(err, ErrInvalidPartialReturn) {
		t.Fatalf("blank order error=%v", err)
	}
	if _, err := svc.ListPartialReturns(ctx, "missing-order"); !errors.Is(err, orders.ErrNotFound) {
		t.Fatalf("missing order error=%v", err)
	}
}
