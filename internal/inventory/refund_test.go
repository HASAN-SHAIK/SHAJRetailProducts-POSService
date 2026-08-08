package inventory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func openRefundTestDB(t *testing.T) *database.DB {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	if err := db.Migrate(ctx); err != nil { db.Close(); t.Fatal(err) }
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedRefundInventory(t *testing.T, db *database.DB, productID string, onHand int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at)
		VALUES(?,?,'unit',1,0,1,1,'2026-08-09T00:00:00Z')`, productID, "Refund Product"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at)
		VALUES('store-1',?,?,0,2,'2026-08-09T00:00:00Z')`, productID, onHand); err != nil {
		t.Fatal(err)
	}
}

func testRefundOrder(productID string) orders.Order {
	return orders.Order{
		ID: "order-1",
		StoreID: "store-1",
		Items: []orders.Item{{
			ID: "order-item-1",
			LineNo: 1,
			ProductID: productID,
			QuantityMilli: 1000,
		}},
	}
}

func TestApplySaleReturnTxRestoresIssuedInventoryAndEmitsEvent(t *testing.T) {
	ctx := context.Background()
	db := openRefundTestDB(t)
	seedRefundInventory(t, db, "product-1", -1000)
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inventory_movements(
			id,store_id,product_id,movement_type,quantity_delta_milli,
			reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at
		) VALUES('sale-issue-1','store-1','product-1','sale_issue',-1000,
			'sale_order','order-1','order-item-1',-1000,'2026-08-09T00:00:00Z','2026-08-09T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	service := New(db)
	order := testRefundOrder("product-1")
	if err := db.WithTx(ctx, func(tx *sql.Tx) error { return service.ApplySaleReturnTx(ctx, tx, order) }); err != nil {
		t.Fatal(err)
	}

	var onHand int64
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 0 { t.Fatalf("on_hand_milli=%d want=0", onHand) }

	var movementCount int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='order-item-1' AND movement_type='sale_return'`).Scan(&movementCount); err != nil {
		t.Fatal(err)
	}
	if movementCount != 1 { t.Fatalf("sale_return movements=%d want=1", movementCount) }

	var eventCount int
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE event_type='inventory.movement.recorded'
		  AND ordering_key='sales_order:order-1'
		  AND payload_json LIKE '%sale_return%'`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 { t.Fatalf("sale_return events=%d want=1", eventCount) }
}

func TestApplySaleReturnTxIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openRefundTestDB(t)
	seedRefundInventory(t, db, "product-1", -1000)
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inventory_movements(
			id,store_id,product_id,movement_type,quantity_delta_milli,
			reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at
		) VALUES('sale-issue-1','store-1','product-1','sale_issue',-1000,
			'sale_order','order-1','order-item-1',-1000,'2026-08-09T00:00:00Z','2026-08-09T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	service := New(db)
	order := testRefundOrder("product-1")
	for i := 0; i < 2; i++ {
		if err := db.WithTx(ctx, func(tx *sql.Tx) error { return service.ApplySaleReturnTx(ctx, tx, order) }); err != nil {
			t.Fatal(err)
		}
	}

	var onHand int64
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 0 { t.Fatalf("on_hand_milli=%d want=0 after replay", onHand) }

	var count int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='order-item-1' AND movement_type='sale_return'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 { t.Fatalf("sale_return movements=%d want=1", count) }
}

func TestApplySaleReturnTxDoesNotInflateUnissuedInventory(t *testing.T) {
	ctx := context.Background()
	db := openRefundTestDB(t)
	seedRefundInventory(t, db, "product-1", 5000)
	service := New(db)
	order := testRefundOrder("product-1")

	if err := db.WithTx(ctx, func(tx *sql.Tx) error { return service.ApplySaleReturnTx(ctx, tx, order) }); err != nil {
		t.Fatal(err)
	}

	var onHand int64
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 5000 { t.Fatalf("on_hand_milli=%d want=5000", onHand) }

	var count int
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inventory_movements WHERE movement_type='sale_return'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 { t.Fatalf("sale_return movements=%d want=0", count) }
}
