package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1EffectivePricePrefersStoreThenPriorityAndValidity(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(id,name,is_active,allow_manual_price,track_inventory,version,updated_at)
		VALUES(?,?,?,?,?,?,?)`, "301", "Price Precedence Item", 1, 0, 0, 1, now.Format(time.RFC3339Nano))
	if err != nil { t.Fatal(err) }

	insertPrice := func(id string, store any, amount, priority int64, validFrom, validTo any, updatedAt time.Time) {
		t.Helper()
		_, err := db.SQL().ExecContext(ctx, `
			INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,valid_from,valid_to,priority,version,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, "301", store, "INR", amount, 1, validFrom, validTo, priority, 1, updatedAt.Format(time.RFC3339Nano))
		if err != nil { t.Fatal(err) }
	}

	insertPrice("global-high", nil, 12000, 999, nil, nil, now.Add(-time.Minute))
	insertPrice("store-low", "store-1", 9000, 10, nil, nil, now.Add(-2*time.Minute))
	insertPrice("store-high", "store-1", 9500, 20, nil, nil, now.Add(-3*time.Minute))
	insertPrice("store-expired", "store-1", 1000, 1000, now.Add(-2*time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano), now)
	insertPrice("store-future", "store-1", 2000, 2000, now.Add(time.Hour).Format(time.RFC3339Nano), nil, now)

	product, err := NewRepository(db).GetProduct(ctx, "301", "store-1")
	if err != nil { t.Fatal(err) }
	if product.Price == nil || product.Price.AmountMinor != 9500 {
		t.Fatalf("expected valid highest-priority store price 9500, got %+v", product.Price)
	}

	otherStore, err := NewRepository(db).GetProduct(ctx, "301", "store-2")
	if err != nil { t.Fatal(err) }
	if otherStore.Price == nil || otherStore.Price.AmountMinor != 12000 {
		t.Fatalf("expected global fallback 12000 for other store, got %+v", otherStore.Price)
	}
}

func TestV1EffectivePriceUsesNewestUpdateAsStableTieBreaker(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?)`, "302", "Tie Break Item", 1, 0, 0, 1, now.Format(time.RFC3339Nano))
	if err != nil { t.Fatal(err) }
	for _, row := range []struct{id string; amount int64; updated time.Time}{{"older", 7000, now.Add(-time.Hour)}, {"newer", 7100, now}} {
		_, err = db.SQL().ExecContext(ctx, `INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, row.id, "302", "store-1", "INR", row.amount, 1, 50, 1, row.updated.Format(time.RFC3339Nano))
		if err != nil { t.Fatal(err) }
	}
	product, err := NewRepository(db).GetProduct(ctx, "302", "store-1")
	if err != nil { t.Fatal(err) }
	if product.Price == nil || product.Price.AmountMinor != 7100 {
		t.Fatalf("expected newest store price on equal priority, got %+v", product.Price)
	}
}
