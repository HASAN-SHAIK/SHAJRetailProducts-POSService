package orders

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1CatalogUOMBoundaryUsesMilliscaleQuantityWithoutInferringUnits(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"101", "Weighted Catalog Item", "unit", 1, 0, 0, 1, "2026-08-14T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog product: %v", err)
	}
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"price-101", "101", "store-1", "INR", 10000, 1, 100, 1, "2026-08-14T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog price: %v", err)
	}

	service := New(db, catalog.NewRepository(db))
	order, err := service.Create(ctx, CreateInput{
		ClientOrderID: "uom-boundary-order",
		StoreID:       "store-1",
		Currency:      "INR",
		Items: []ItemInput{{
			ProductID:     ExternalID("101"),
			QuantityMilli: 500,
		}},
	})
	if err != nil {
		t.Fatalf("create fractional-quantity order: %v", err)
	}
	if len(order.Items) != 1 || order.Items[0].QuantityMilli != 500 {
		t.Fatalf("milliscale quantity was not preserved: %+v", order.Items)
	}
	if order.SubtotalMinor != 5000 || order.TotalMinor != 5000 {
		t.Fatalf("fractional quantity pricing mismatch: subtotal=%d total=%d", order.SubtotalMinor, order.TotalMinor)
	}

	product, err := catalog.NewRepository(db).GetProduct(ctx, "101", "store-1")
	if err != nil {
		t.Fatalf("reload catalog product: %v", err)
	}
	if product.UnitOfMeasure == nil || *product.UnitOfMeasure != "unit" {
		t.Fatalf("expected current Central placeholder UOM to remain a label only: %#v", product.UnitOfMeasure)
	}
}
