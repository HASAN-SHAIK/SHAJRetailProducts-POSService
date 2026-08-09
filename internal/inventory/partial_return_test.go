package inventory

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func seedPartialReturnIssue(t *testing.T, db interface {
	SQL() *sql.DB
}, productID, orderID, orderItemID string, quantityMilli int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inventory_movements(
			id,store_id,product_id,movement_type,quantity_delta_milli,
			reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at
		) VALUES(?, 'store-1', ?, 'sale_issue', ?, 'sale_order', ?, ?, ?,
			'2026-08-09T00:00:00Z','2026-08-09T00:00:00Z')`,
		"sale-issue-"+orderItemID, productID, -quantityMilli, orderID, orderItemID, -quantityMilli); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPartialSaleReturnTxRestoresRequestedQuantitiesAcrossOperations(t *testing.T) {
	ctx := context.Background()
	db := openRefundTestDB(t)
	seedRefundInventory(t, db, "product-1", -1000)
	order := testRefundOrder("product-1")
	seedPartialReturnIssue(t, db, "product-1", order.ID, order.Items[0].ID, 1000)
	service := New(db)

	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return service.ApplyPartialSaleReturnTx(ctx, tx, order, "return-1", []PartialSaleReturnLine{{
			OrderItemID: order.Items[0].ID, QuantityMilli: 400,
		}})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return service.ApplyPartialSaleReturnTx(ctx, tx, order, "return-2", []PartialSaleReturnLine{{
			OrderItemID: order.Items[0].ID, QuantityMilli: 600,
		}})
	}); err != nil {
		t.Fatal(err)
	}

	var onHand int64
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 0 {
		t.Fatalf("on_hand_milli=%d want=0", onHand)
	}

	var movementCount, returnedMilli, eventCount int64
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(quantity_delta_milli),0)
		FROM inventory_movements
		WHERE order_item_id=? AND movement_type='sale_return'`, order.Items[0].ID).Scan(&movementCount, &returnedMilli); err != nil {
		t.Fatal(err)
	}
	if movementCount != 2 || returnedMilli != 1000 {
		t.Fatalf("sale_return movements=%d returned=%d want=2/1000", movementCount, returnedMilli)
	}
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE event_type='inventory.movement.recorded'
		  AND ordering_key='sales_order:order-1'
		  AND payload_json LIKE '%sale_return%'`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 {
		t.Fatalf("sale_return events=%d want=2", eventCount)
	}
}

func TestApplyPartialSaleReturnTxReplayIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openRefundTestDB(t)
	seedRefundInventory(t, db, "product-1", -1000)
	order := testRefundOrder("product-1")
	seedPartialReturnIssue(t, db, "product-1", order.ID, order.Items[0].ID, 1000)
	service := New(db)
	apply := func(quantity int64) error {
		return db.WithTx(ctx, func(tx *sql.Tx) error {
			return service.ApplyPartialSaleReturnTx(ctx, tx, order, "return-1", []PartialSaleReturnLine{{
				OrderItemID: order.Items[0].ID, QuantityMilli: quantity,
			}})
		})
	}

	if err := apply(400); err != nil {
		t.Fatal(err)
	}
	if err := apply(400); err != nil {
		t.Fatalf("identical replay failed: %v", err)
	}
	if err := apply(500); !errors.Is(err, ErrPartialSaleReturnReplayMismatch) {
		t.Fatalf("mismatched replay error=%v want=%v", err, ErrPartialSaleReturnReplayMismatch)
	}

	var onHand int64
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != -600 {
		t.Fatalf("on_hand_milli=%d want=-600 after replay", onHand)
	}

	var count int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inventory_movements WHERE order_item_id=? AND movement_type='sale_return'`, order.Items[0].ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("sale_return movements=%d want=1", count)
	}
}

func TestApplyPartialSaleReturnTxRejectsOverRestoreAndRollsBack(t *testing.T) {
	ctx := context.Background()
	db := openRefundTestDB(t)
	seedRefundInventory(t, db, "product-1", -1000)
	order := testRefundOrder("product-1")
	seedPartialReturnIssue(t, db, "product-1", order.ID, order.Items[0].ID, 1000)
	service := New(db)

	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return service.ApplyPartialSaleReturnTx(ctx, tx, order, "return-too-much", []PartialSaleReturnLine{{
			OrderItemID: order.Items[0].ID, QuantityMilli: 1001,
		}})
	}); !errors.Is(err, ErrPartialSaleReturnQuantityExceeded) {
		t.Fatalf("error=%v want=%v", err, ErrPartialSaleReturnQuantityExceeded)
	}

	var onHand int64
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != -1000 {
		t.Fatalf("on_hand_milli=%d want=-1000 after rejected return", onHand)
	}

	var count int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inventory_movements WHERE movement_type='sale_return'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("sale_return movements=%d want=0", count)
	}
}

func TestApplyPartialSaleReturnTxSkipsUnissuedInventory(t *testing.T) {
	ctx := context.Background()
	db := openRefundTestDB(t)
	seedRefundInventory(t, db, "product-1", 5000)
	order := testRefundOrder("product-1")
	service := New(db)

	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return service.ApplyPartialSaleReturnTx(ctx, tx, order, "return-untracked", []PartialSaleReturnLine{{
			OrderItemID: order.Items[0].ID, QuantityMilli: 400,
		}})
	}); err != nil {
		t.Fatal(err)
	}

	var onHand int64
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 5000 {
		t.Fatalf("on_hand_milli=%d want=5000", onHand)
	}
}
