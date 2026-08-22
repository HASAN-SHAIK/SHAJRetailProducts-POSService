package orders

import (
	"context"
	"strings"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestCreateRejectsInactiveCustomerIdentityAndPreservesAnonymousSale(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"inactive-customer-product", "Identity Product", "unit", 1, 0, 0, 1, "2026-08-21T14:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert product: %v", err)
	}
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"inactive-customer-price", "inactive-customer-product", "store-1", "INR", 10000, 1, 100, 1, "2026-08-21T14:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert price: %v", err)
	}
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO customers(
			id,name,credit_limit_minor,outstanding_minor,currency,status,created_at,updated_at,local_version,sync_state
		) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"inactive-customer-1", "Inactive Customer", 0, 0, "INR", "inactive", "2026-08-21T14:00:00Z", "2026-08-21T14:00:00Z", 1, "synced",
	)
	if err != nil {
		t.Fatalf("insert inactive customer: %v", err)
	}

	service := New(db, catalog.NewRepository(db))
	inactiveID := ExternalID("inactive-customer-1")
	_, err = service.Create(ctx, CreateInput{
		ClientOrderID: "inactive-customer-order",
		StoreID:       "store-1",
		CustomerID:    &inactiveID,
		Currency:      "INR",
		Items: []ItemInput{{
			ProductID:     ExternalID("inactive-customer-product"),
			QuantityMilli: 1000,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "customer_inactive") {
		t.Fatalf("expected inactive customer rejection, got %v", err)
	}

	var rejectedCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE client_order_id=?`, "inactive-customer-order").Scan(&rejectedCount); err != nil {
		t.Fatalf("count rejected order: %v", err)
	}
	if rejectedCount != 0 {
		t.Fatalf("inactive-customer order persisted: count=%d", rejectedCount)
	}

	anonymous, err := service.Create(ctx, CreateInput{
		ClientOrderID: "anonymous-order-after-inactive-rejection",
		StoreID:       "store-1",
		Currency:      "INR",
		Items: []ItemInput{{
			ProductID:     ExternalID("inactive-customer-product"),
			QuantityMilli: 1000,
		}},
	})
	if err != nil {
		t.Fatalf("anonymous order should remain valid: %v", err)
	}
	if anonymous.CustomerID != nil {
		t.Fatalf("anonymous order customer id = %#v, want nil", anonymous.CustomerID)
	}
}
