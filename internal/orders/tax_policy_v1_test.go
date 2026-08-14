package orders

import (
	"context"
	"errors"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1DeterministicGSTOverridesCallerTaxSnapshot(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,tax_code,gst_rate_bps,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "401", "GST Item", "HSN401", 1800, 1, 0, 0, 1, "2026-08-14T18:30:00Z")
	if err != nil { t.Fatal(err) }
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "price-401", "401", "store-1", "INR", 10000, 1, 100, 1, "2026-08-14T18:30:00Z")
	if err != nil { t.Fatal(err) }

	service := New(db, catalog.NewRepository(db))
	service.SetTaxPolicy(func(context.Context) (TaxPolicy, error) {
		return TaxPolicy{Enabled: true, Mode: "EXCLUSIVE", RoundingMode: "HALF_UP"}, nil
	})
	order, err := service.Create(ctx, CreateInput{ClientOrderID: "gst-exclusive", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("401"), QuantityMilli: 1000, TaxMinor: 9999}}})
	if err != nil { t.Fatalf("create exclusive GST order: %v", err) }
	if order.TaxMinor != 1800 || order.TotalMinor != 11800 { t.Fatalf("exclusive GST mismatch: tax=%d total=%d", order.TaxMinor, order.TotalMinor) }
	if len(order.Items) != 1 || order.Items[0].TaxMinor != 1800 || order.Items[0].TaxCode == nil || *order.Items[0].TaxCode != "HSN401" { t.Fatalf("line GST snapshot mismatch: %+v", order.Items) }

	service.SetTaxPolicy(func(context.Context) (TaxPolicy, error) {
		return TaxPolicy{Enabled: true, Mode: "INCLUSIVE", RoundingMode: "HALF_UP"}, nil
	})
	order, err = service.Create(ctx, CreateInput{ClientOrderID: "gst-inclusive", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("401"), QuantityMilli: 1000}}})
	if err != nil { t.Fatalf("create inclusive GST order: %v", err) }
	if order.TaxMinor != 1525 || order.TotalMinor != 10000 { t.Fatalf("inclusive GST mismatch: tax=%d total=%d", order.TaxMinor, order.TotalMinor) }
}

func TestV1GSTUsesDiscountedAmountAndHalfUpMinorRounding(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,gst_rate_bps,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?,?)`, "402", "Discount GST Item", 500, 1, 0, 0, 1, "2026-08-14T18:30:00Z")
	if err != nil { t.Fatal(err) }
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "price-402", "402", "store-1", "INR", 10, 0, 100, 1, "2026-08-14T18:30:00Z")
	if err != nil { t.Fatal(err) }

	service := New(db, catalog.NewRepository(db))
	service.SetDiscountPolicy(func(context.Context) (DiscountPolicy, error) { return DiscountPolicy{Allowed: true, MaxPercent: 20}, nil })
	service.SetTaxPolicy(func(context.Context) (TaxPolicy, error) { return TaxPolicy{Enabled: true, Mode: "EXCLUSIVE", RoundingMode: "HALF_UP"}, nil })
	order, err := service.Create(ctx, CreateInput{ClientOrderID: "gst-half-up", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("402"), QuantityMilli: 1000}}})
	if err != nil { t.Fatalf("create half-up order: %v", err) }
	if order.TaxMinor != 1 || order.TotalMinor != 11 { t.Fatalf("HALF_UP boundary mismatch: tax=%d total=%d", order.TaxMinor, order.TotalMinor) }
}

func TestV1GSTDisabledProducesZeroTaxAndPolicyFailuresFailClosed(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,gst_rate_bps,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?,?)`, "403", "GST Disabled Item", 1800, 1, 0, 0, 1, "2026-08-14T18:30:00Z")
	if err != nil { t.Fatal(err) }
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "price-403", "403", "store-1", "INR", 10000, 1, 100, 1, "2026-08-14T18:30:00Z")
	if err != nil { t.Fatal(err) }

	service := New(db, catalog.NewRepository(db))
	service.SetTaxPolicy(func(context.Context) (TaxPolicy, error) { return TaxPolicy{Enabled: false, Mode: "INCLUSIVE", RoundingMode: "HALF_UP"}, nil })
	order, err := service.Create(ctx, CreateInput{ClientOrderID: "gst-disabled", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("403"), QuantityMilli: 1000, TaxMinor: 5000}}})
	if err != nil { t.Fatalf("create GST-disabled order: %v", err) }
	if order.TaxMinor != 0 || order.TotalMinor != 10000 { t.Fatalf("GST-disabled mismatch: tax=%d total=%d", order.TaxMinor, order.TotalMinor) }

	service.SetTaxPolicy(func(context.Context) (TaxPolicy, error) { return TaxPolicy{}, errors.New("config unavailable") })
	_, err = service.Create(ctx, CreateInput{ClientOrderID: "gst-policy-error", StoreID: "store-1", Items: []ItemInput{{ProductID: ExternalID("403"), QuantityMilli: 1000}}})
	if err == nil || errors.Is(err, ErrInvalidOrder) { t.Fatalf("expected explicit tax policy load failure, got %v", err) }
}
