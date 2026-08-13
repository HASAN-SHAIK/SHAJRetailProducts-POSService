package inventory

import (
    "context"
    "database/sql"
    "errors"
    "path/filepath"
    "testing"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func openSaleInventoryTestDB(t *testing.T) *database.DB {
    t.Helper()
    ctx := context.Background()
    db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
    if err != nil { t.Fatal(err) }
    if err := db.Migrate(ctx); err != nil { _ = db.Close(); t.Fatal(err) }
    t.Cleanup(func() { _ = db.Close() })
    return db
}

func seedSaleInventory(t *testing.T, db *database.DB, productID string, onHand int64) {
    t.Helper()
    ctx := context.Background()
    if _, err := db.SQL().ExecContext(ctx, `
        INSERT INTO catalog_products(id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at)
        VALUES(?,?,'unit',1,0,1,1,'2026-08-13T00:00:00Z')`, productID, "Sale Product"); err != nil {
        t.Fatal(err)
    }
    if _, err := db.SQL().ExecContext(ctx, `
        INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at)
        VALUES('store-1',?,?,0,1,'2026-08-13T00:00:00Z')`, productID, onHand); err != nil {
        t.Fatal(err)
    }
}

func saleInventoryOrder(productID string, quantityMilli int64) orders.Order {
    return orders.Order{
        ID: "order-sale-1",
        StoreID: "store-1",
        Items: []orders.Item{{
            ID: "order-item-sale-1",
            LineNo: 1,
            ProductID: productID,
            QuantityMilli: quantityMilli,
        }},
    }
}

func TestApplySaleTxRejectsInsufficientInventoryWithoutMovementOrOutbox(t *testing.T) {
    ctx := context.Background()
    db := openSaleInventoryTestDB(t)
    seedSaleInventory(t, db, "product-1", 500)

    service := New(db)
    err := db.WithTx(ctx, func(tx *sql.Tx) error {
        return service.ApplySaleTx(ctx, tx, saleInventoryOrder("product-1", 1000))
    })
    if !errors.Is(err, ErrInsufficientInventory) {
        t.Fatalf("ApplySaleTx error=%v want ErrInsufficientInventory", err)
    }

    var onHand int64
    if err := db.SQL().QueryRowContext(ctx, `
        SELECT on_hand_milli FROM inventory_balances
        WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
        t.Fatal(err)
    }
    if onHand != 500 { t.Fatalf("on_hand_milli=%d want=500", onHand) }

    var movementCount int
    if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements`).Scan(&movementCount); err != nil {
        t.Fatal(err)
    }
    if movementCount != 0 { t.Fatalf("inventory movements=%d want=0", movementCount) }

    var outboxCount int
    if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE event_type='inventory.movement.recorded'`).Scan(&outboxCount); err != nil {
        t.Fatal(err)
    }
    if outboxCount != 0 { t.Fatalf("inventory outbox events=%d want=0", outboxCount) }
}

func TestApplySaleTxAllowsExactAvailableOnHandAndNeverGoesNegative(t *testing.T) {
    ctx := context.Background()
    db := openSaleInventoryTestDB(t)
    seedSaleInventory(t, db, "product-1", 1000)

    service := New(db)
    if err := db.WithTx(ctx, func(tx *sql.Tx) error {
        return service.ApplySaleTx(ctx, tx, saleInventoryOrder("product-1", 1000))
    }); err != nil {
        t.Fatal(err)
    }

    var onHand int64
    if err := db.SQL().QueryRowContext(ctx, `
        SELECT on_hand_milli FROM inventory_balances
        WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
        t.Fatal(err)
    }
    if onHand != 0 { t.Fatalf("on_hand_milli=%d want=0", onHand) }

    var delta, after int64
    if err := db.SQL().QueryRowContext(ctx, `
        SELECT quantity_delta_milli,balance_after_milli FROM inventory_movements
        WHERE order_item_id='order-item-sale-1' AND movement_type='sale_issue'`).Scan(&delta, &after); err != nil {
        t.Fatal(err)
    }
    if delta != -1000 || after != 0 {
        t.Fatalf("movement delta=%d after=%d want delta=-1000 after=0", delta, after)
    }
}