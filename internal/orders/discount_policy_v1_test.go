package orders

import (
	"context"
	"errors"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1LineDiscountRequiresCentralPermissionAndLimit(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?)`, "301", "Discount Item", 1, 0, 0, 1, "2026-08-14T13:30:00Z")
	if err != nil { t.Fatal(err) }
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "price-301", "301", "store-1", "INR", 10000, 1, 100, 1, "2026-08-14T13:30:00Z")
	if err != nil { t.Fatal(err) }

	service := New(db, catalog.NewRepository(db))
	service.SetDiscountPolicy(func(context.Context) (DiscountPolicy, error) {
		return DiscountPolicy{Allowed: false, MaxPercent: 20}, nil
	})
	_, err = service.Create(ctx, CreateInput{ClientOrderID: "discount-disabled", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("301"), QuantityMilli: 1000, DiscountMinor: 1000}}})
	if !errors.Is(err, ErrDiscountNotAllowed) { t.Fatalf("expected typed disabled discount rejection, got %v", err) }

	service.SetDiscountPolicy(func(context.Context) (DiscountPolicy, error) {
		return DiscountPolicy{Allowed: true, MaxPercent: 20}, nil
	})
	_, err = service.Create(ctx, CreateInput{ClientOrderID: "discount-over-limit", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("301"), QuantityMilli: 1000, DiscountMinor: 2001}}})
	if !errors.Is(err, ErrDiscountLimitExceeded) { t.Fatalf("expected typed discount limit rejection, got %v", err) }

	order, err := service.Create(ctx, CreateInput{ClientOrderID: "discount-at-limit", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("301"), QuantityMilli: 1000, DiscountMinor: 2000}}})
	if err != nil { t.Fatalf("expected discount at Central limit to succeed: %v", err) }
	if got := order.DiscountMinor; got != 2000 { t.Fatalf("discount snapshot mismatch: got %d", got) }
	if got := order.TotalMinor; got != 8000 { t.Fatalf("discounted total mismatch: got %d", got) }
}

func TestV1LineDiscountPolicyFailureFailsClosed(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?)`, "302", "Discount Policy Failure Item", 1, 0, 0, 1, "2026-08-14T13:30:00Z")
	if err != nil { t.Fatal(err) }
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "price-302", "302", "store-1", "INR", 10000, 1, 100, 1, "2026-08-14T13:30:00Z")
	if err != nil { t.Fatal(err) }

	service := New(db, catalog.NewRepository(db))
	service.SetDiscountPolicy(func(context.Context) (DiscountPolicy, error) {
		return DiscountPolicy{}, errors.New("config unavailable")
	})
	_, err = service.Create(ctx, CreateInput{ClientOrderID: "discount-policy-error", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("302"), QuantityMilli: 1000, DiscountMinor: 1000}}})
	if !errors.Is(err, ErrPricingPolicyUnavailable) { t.Fatalf("expected typed policy load failure, got %v", err) }
}
