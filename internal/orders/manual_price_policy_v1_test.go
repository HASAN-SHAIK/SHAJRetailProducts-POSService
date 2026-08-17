package orders

import (
	"context"
	"errors"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1ManualPriceOverrideRequiresCentralPolicy(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"201", "Manual Price Item", "unit", 1, 1, 0, 1, "2026-08-14T12:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog product: %v", err)
	}
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"price-201", "201", "store-1", "INR", 10000, 1, 100, 1, "2026-08-14T12:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog price: %v", err)
	}

	service := New(db, catalog.NewRepository(db))
	service.SetPriceOverridePolicy(func(context.Context) (bool, error) { return false, nil })
	override := int64(9000)
	_, err = service.Create(ctx, CreateInput{
		ClientOrderID: "manual-price-denied",
		StoreID:       "store-1",
		Currency:      "INR",
		Items: []ItemInput{{ProductID: ExternalID("201"), QuantityMilli: 1000, UnitPriceMinor: &override}},
	})
	if !errors.Is(err, ErrPriceOverrideNotAllowed) {
		t.Fatalf("expected typed Central policy rejection, got %v", err)
	}

	service.SetPriceOverridePolicy(func(context.Context) (bool, error) { return true, nil })
	order, err := service.Create(ctx, CreateInput{
		ClientOrderID: "manual-price-allowed",
		StoreID:       "store-1",
		Currency:      "INR",
		Items: []ItemInput{{ProductID: ExternalID("201"), QuantityMilli: 1000, UnitPriceMinor: &override}},
	})
	if err != nil {
		t.Fatalf("expected Central policy to allow manual price override: %v", err)
	}
	if got := order.Items[0].UnitPriceMinor; got != 9000 {
		t.Fatalf("manual price snapshot mismatch: got %d", got)
	}
}

func TestV1ManualPricePolicyFailureFailsClosed(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?)`, "202", "Policy Failure Item", 1, 1, 0, 1, "2026-08-14T12:00:00Z")
	if err != nil { t.Fatal(err) }
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "price-202", "202", "store-1", "INR", 10000, 1, 100, 1, "2026-08-14T12:00:00Z")
	if err != nil { t.Fatal(err) }

	service := New(db, catalog.NewRepository(db))
	service.SetPriceOverridePolicy(func(context.Context) (bool, error) { return false, errors.New("config unavailable") })
	override := int64(9000)
	_, err = service.Create(ctx, CreateInput{ClientOrderID: "manual-price-policy-error", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("202"), QuantityMilli: 1000, UnitPriceMinor: &override}}})
	if !errors.Is(err, ErrPricingPolicyUnavailable) {
		t.Fatalf("expected typed policy load failure, got %v", err)
	}
}
