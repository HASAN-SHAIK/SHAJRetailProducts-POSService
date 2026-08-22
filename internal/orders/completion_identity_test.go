package orders

import (
    "context"
    "encoding/json"
    "testing"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestCompleteWithPersistsAuthenticatedCompleterInOrderAndSnapshot(t *testing.T) {
    db := testutil.OpenDatabase(t)
    ctx := context.Background()

    _, err := db.SQL().ExecContext(ctx, `
        INSERT INTO catalog_products(
            id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
        ) VALUES(?,?,?,?,?,?,?,?)`,
        "completer-product", "Completer Product", "unit", 1, 0, 0, 1, "2026-08-21T11:00:00Z",
    )
    if err != nil {
        t.Fatalf("insert catalog product: %v", err)
    }
    _, err = db.SQL().ExecContext(ctx, `
        INSERT INTO catalog_prices(
            id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
        ) VALUES(?,?,?,?,?,?,?,?,?)`,
        "completer-price", "completer-product", "store-1", "INR", 9900, 1, 100, 1, "2026-08-21T11:00:00Z",
    )
    if err != nil {
        t.Fatalf("insert catalog price: %v", err)
    }

    service := New(db, catalog.NewRepository(db))
    created, err := service.Create(WithCreatorUserID(ctx, "staff-creator-1"), CreateInput{
        ClientOrderID: "completer-order-1",
        StoreID:       "store-1",
        Currency:      "INR",
        Items: []ItemInput{{
            ProductID:     ExternalID("completer-product"),
            QuantityMilli: 1000,
        }},
    })
    if err != nil {
        t.Fatalf("create order: %v", err)
    }

    completed, err := service.CompleteWith(WithCreatorUserID(ctx, "staff-cashier-2"), created.ID)
    if err != nil {
        t.Fatalf("complete order: %v", err)
    }
    if completed.CompletedByUserID == nil || *completed.CompletedByUserID != "staff-cashier-2" {
        t.Fatalf("expected returned completer staff-cashier-2, got %#v", completed.CompletedByUserID)
    }
    if completed.CreatedByUserID == nil || *completed.CreatedByUserID != "staff-creator-1" {
        t.Fatalf("completion must preserve creator staff-creator-1, got %#v", completed.CreatedByUserID)
    }

    var persistedCompleter string
    if err := db.SQL().QueryRowContext(ctx,
        `SELECT completed_by_user_id FROM sales_orders WHERE id=?`, created.ID,
    ).Scan(&persistedCompleter); err != nil {
        t.Fatalf("read persisted completer: %v", err)
    }
    if persistedCompleter != "staff-cashier-2" {
        t.Fatalf("expected persisted completer staff-cashier-2, got %q", persistedCompleter)
    }

    var snapshotRaw string
    if err := db.SQL().QueryRowContext(ctx,
        `SELECT snapshot_json FROM sales_order_snapshots WHERE order_id=? AND version=?`, created.ID, completed.Version,
    ).Scan(&snapshotRaw); err != nil {
        t.Fatalf("read completion snapshot: %v", err)
    }
    var snapshot Order
    if err := json.Unmarshal([]byte(snapshotRaw), &snapshot); err != nil {
        t.Fatalf("decode completion snapshot: %v", err)
    }
    if snapshot.CompletedByUserID == nil || *snapshot.CompletedByUserID != "staff-cashier-2" {
        t.Fatalf("expected snapshot completer staff-cashier-2, got %#v", snapshot.CompletedByUserID)
    }
    if snapshot.CreatedByUserID == nil || *snapshot.CreatedByUserID != "staff-creator-1" {
        t.Fatalf("completion snapshot must preserve creator staff-creator-1, got %#v", snapshot.CreatedByUserID)
    }

    readBack, err := service.Get(ctx, created.ID)
    if err != nil {
        t.Fatalf("get completed order: %v", err)
    }
    if readBack.CompletedByUserID == nil || *readBack.CompletedByUserID != "staff-cashier-2" {
        t.Fatalf("expected read model completer staff-cashier-2, got %#v", readBack.CompletedByUserID)
    }
}
