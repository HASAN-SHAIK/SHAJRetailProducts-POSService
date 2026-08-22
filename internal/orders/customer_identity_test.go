package orders

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestCreatePreservesActiveCustomerRelationshipAndAllowsAnonymousSale(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"customer-identity-product", "Customer Identity Product", "unit", 1, 0, 0, 1, "2026-08-21T13:45:00Z",
	); err != nil {
		t.Fatalf("insert catalog product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"customer-identity-price", "customer-identity-product", "store-1", "INR", 15000, 1, 100, 1, "2026-08-21T13:45:00Z",
	); err != nil {
		t.Fatalf("insert catalog price: %v", err)
	}

	customerRepo := customer.NewRepository(db)
	createdCustomer, err := customerRepo.Create(ctx, customer.UpsertInput{
		Name:     "Identity Customer",
		Currency: "INR",
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	service := New(db, catalog.NewRepository(db))
	customerID := ExternalID(createdCustomer.ID)
	linked, err := service.Create(WithCreatorUserID(ctx, "staff-customer-link"), CreateInput{
		ClientOrderID: "customer-linked-order",
		StoreID:       "store-1",
		CustomerID:    &customerID,
		Currency:      "INR",
		Items: []ItemInput{{
			ProductID:     ExternalID("customer-identity-product"),
			QuantityMilli: 1000,
		}},
	})
	if err != nil {
		t.Fatalf("create customer-linked order: %v", err)
	}
	if linked.CustomerID == nil || *linked.CustomerID != createdCustomer.ID {
		t.Fatalf("expected linked customer %q, got %#v", createdCustomer.ID, linked.CustomerID)
	}

	readBack, err := service.Get(ctx, linked.ID)
	if err != nil {
		t.Fatalf("read customer-linked order: %v", err)
	}
	if readBack.CustomerID == nil || *readBack.CustomerID != createdCustomer.ID {
		t.Fatalf("expected persisted customer %q, got %#v", createdCustomer.ID, readBack.CustomerID)
	}

	anonymous, err := service.Create(WithCreatorUserID(ctx, "staff-anonymous-sale"), CreateInput{
		ClientOrderID: "anonymous-order",
		StoreID:       "store-1",
		Currency:      "INR",
		Items: []ItemInput{{
			ProductID:     ExternalID("customer-identity-product"),
			QuantityMilli: 1000,
		}},
	})
	if err != nil {
		t.Fatalf("create anonymous order: %v", err)
	}
	if anonymous.CustomerID != nil {
		t.Fatalf("anonymous order must keep customer_id null, got %#v", anonymous.CustomerID)
	}

	anonymousReadBack, err := service.Get(ctx, anonymous.ID)
	if err != nil {
		t.Fatalf("read anonymous order: %v", err)
	}
	if anonymousReadBack.CustomerID != nil {
		t.Fatalf("persisted anonymous order must keep customer_id null, got %#v", anonymousReadBack.CustomerID)
	}
}
