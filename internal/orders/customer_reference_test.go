package orders

import (
	"context"
	"errors"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestCreateRejectsMissingCustomerReference(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"101", "Catalog Item", "unit", 1, 0, 0, 1, "2026-08-14T09:00:00Z",
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

	customerID := ExternalID("1")
	service := New(db, catalog.NewRepository(db))
	_, err = service.Create(ctx, CreateInput{
		ClientOrderID: "missing-customer-order",
		StoreID:       "store-1",
		CustomerID:    &customerID,
		Currency:      "INR",
		Items: []ItemInput{{
			ProductID:     ExternalID("101"),
			QuantityMilli: 1000,
		}},
	})
	if !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("expected missing customer reference to be rejected, got %v", err)
	}
}
