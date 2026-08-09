package refunds

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestPartialReturnLedgerPersistsHistoryAndReplaysIdempotently(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)

	record := PartialReturnLedgerRecord{
		ID:               "return-1",
		OrderID:          "ord-refund-full",
		ApprovedByUserID: "manager-1",
		Reason:           "damaged item",
		RefundMinor:      2500,
		Lines: []PartialReturnLedgerLine{
			{OrderItemID: "item-refund-full", QuantityMilli: 250, RefundMinor: 2500},
		},
	}

	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		created, err := AppendPartialReturnTx(ctx, tx, record)
		if err != nil { return err }
		if !created { t.Fatal("first append must create ledger record") }
		history, err := LoadPartialReturnHistoryTx(ctx, tx, record.OrderID)
		if err != nil { return err }
		got := history["item-refund-full"]
		if got.QuantityMilli != 250 || got.RefundMinor != 2500 {
			t.Fatalf("history=%+v", got)
		}
		created, err = AppendPartialReturnTx(ctx, tx, record)
		if err != nil { return err }
		if created { t.Fatal("semantic replay must be idempotent") }
		return nil
	}); err != nil { t.Fatal(err) }

	var headers, lines int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_partial_returns WHERE id='return-1'`).Scan(&headers); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_partial_return_lines WHERE return_id='return-1'`).Scan(&lines); err != nil { t.Fatal(err) }
	if headers != 1 || lines != 1 { t.Fatalf("headers=%d lines=%d", headers, lines) }
}

func TestPartialReturnLedgerRejectsConflictingReplay(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)

	record := PartialReturnLedgerRecord{
		ID: "return-conflict", OrderID: "ord-refund-full", ApprovedByUserID: "manager-1", Reason: "damaged item", RefundMinor: 1000,
		Lines: []PartialReturnLedgerLine{{OrderItemID: "item-refund-full", QuantityMilli: 100, RefundMinor: 1000}},
	}
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := AppendPartialReturnTx(ctx, tx, record)
		return err
	}); err != nil { t.Fatal(err) }

	record.Lines[0].QuantityMilli = 200
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := AppendPartialReturnTx(ctx, tx, record)
		if !errors.Is(err, ErrPartialReturnReplayMismatch) {
			t.Fatalf("error=%v want ErrPartialReturnReplayMismatch", err)
		}
		return nil
	}); err != nil { t.Fatal(err) }
}

func TestPartialReturnLedgerRollsBackWithCallerTransaction(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	record := PartialReturnLedgerRecord{
		ID: "return-rollback", OrderID: "ord-refund-full", ApprovedByUserID: "manager-1", Reason: "damaged item", RefundMinor: 1000,
		Lines: []PartialReturnLedgerLine{{OrderItemID: "item-refund-full", QuantityMilli: 100, RefundMinor: 1000}},
	}
	rollback := errors.New("force rollback")
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := AppendPartialReturnTx(ctx, tx, record); err != nil { return err }
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatalf("rollback error=%v", err)
	}

	var count int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_partial_returns WHERE id='return-rollback'`).Scan(&count); err != nil { t.Fatal(err) }
	if count != 0 { t.Fatalf("rolled-back ledger rows=%d", count) }
}

func TestPartialReturnLedgerRejectsRefundTotalMismatch(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	record := PartialReturnLedgerRecord{
		ID: "return-invalid", OrderID: "ord-refund-full", ApprovedByUserID: "manager-1", Reason: "damaged item", RefundMinor: 1001,
		Lines: []PartialReturnLedgerLine{{OrderItemID: "item-refund-full", QuantityMilli: 100, RefundMinor: 1000}},
	}
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := AppendPartialReturnTx(ctx, tx, record)
		if !errors.Is(err, ErrInvalidPartialReturn) { t.Fatalf("error=%v", err) }
		return nil
	}); err != nil { t.Fatal(err) }
}
