package inventory

import (
    "context"
    "database/sql"
    "path/filepath"
    "testing"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func TestV1OversellPolicyAllowsProvisionalNegativeLocalBalance(t *testing.T) {
    ctx := context.Background()
    db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
    if err != nil { t.Fatal(err) }
    if err := db.Migrate(ctx); err != nil { _ = db.Close(); t.Fatal(err) }
    defer db.Close()

    if _, err := db.SQL().ExecContext(ctx, `
        INSERT INTO catalog_products(id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at)
        VALUES('product-oversell-v1','Offline Product','unit',1,0,1,1,'2026-08-14T00:00:00Z')`); err != nil {
        t.Fatal(err)
    }

    order := orders.Order{
        ID: "order-oversell-v1",
        StoreID: "store-1",
        Items: []orders.Item{{
            ID: "order-item-oversell-v1",
            LineNo: 1,
            ProductID: "product-oversell-v1",
            QuantityMilli: 1000,
        }},
    }

    service := New(db)
    if err := db.WithTx(ctx, func(tx *sql.Tx) error {
        return service.ApplySaleTx(ctx, tx, order)
    }); err != nil {
        t.Fatalf("ApplySaleTx rejected the V1 provisional oversell policy: %v", err)
    }

    var onHand int64
    if err := db.SQL().QueryRowContext(ctx, `
        SELECT on_hand_milli FROM inventory_balances
        WHERE store_id='store-1' AND product_id='product-oversell-v1'`).Scan(&onHand); err != nil {
        t.Fatal(err)
    }
    if onHand != -1000 {
        t.Fatalf("on_hand_milli=%d want=-1000", onHand)
    }

    var movementCount, outboxCount int
    if err := db.SQL().QueryRowContext(ctx, `
        SELECT COUNT(*) FROM inventory_movements
        WHERE reference_id='order-oversell-v1' AND movement_type='sale_issue'`).Scan(&movementCount); err != nil {
        t.Fatal(err)
    }
    if err := db.SQL().QueryRowContext(ctx, `
        SELECT COUNT(*) FROM outbox_events
        WHERE event_type='inventory.movement.recorded' AND ordering_key='sales_order:order-oversell-v1'`).Scan(&outboxCount); err != nil {
        t.Fatal(err)
    }
    if movementCount != 1 || outboxCount != 1 {
        t.Fatalf("movement=%d outbox=%d want 1/1", movementCount, outboxCount)
    }
}
