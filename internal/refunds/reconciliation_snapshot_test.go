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

func TestGetReconciliationSnapshotIncludesRefundSyncState(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	svc, _ := newRefundService(db)

	for _, event := range []struct {
		id      string
		version int
		status  string
	}{
		{id: "evt-reconciliation-published", version: 3, status: "published"},
		{id: "evt-reconciliation-pending", version: 4, status: "pending"},
		{id: "evt-reconciliation-dead", version: 5, status: "dead_letter"},
	} {
		if _, err := db.SQL().ExecContext(ctx, `
			INSERT INTO outbox_events(
				id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
				payload_json,metadata_json,status,attempt_count,available_at,created_at
			) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			event.id, "sales_order", "ord-refund-full", event.version, "sale.partial_returned", 1, "sales_order:ord-refund-full",
			"{}", "{}", event.status, 0, "2026-08-09T10:00:00Z", "2026-08-09T10:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
			payload_json,metadata_json,status,attempt_count,available_at,created_at
		) VALUES('evt-other-order','sales_order','ord-other',1,'sale.completed',1,'sales_order:ord-other','{}','{}','dead_letter',0,'2026-08-09T10:00:00Z','2026-08-09T10:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	snapshot, err := svc.GetReconciliationSnapshot(ctx, "ord-refund-full")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.UnpublishedSyncFacts != 2 || snapshot.DeadLetterSyncFacts != 1 {
		t.Fatalf("sync facts=%+v", snapshot)
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
