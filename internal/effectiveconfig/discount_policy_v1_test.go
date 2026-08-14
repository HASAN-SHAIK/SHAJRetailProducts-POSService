package effectiveconfig

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1CachedDiscountPolicyDefaultsClosedAndPersistsCentralLimit(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	store := NewStore(db)

	allowed, err := store.Bool(ctx, "billing.allow_discount", false)
	if err != nil { t.Fatal(err) }
	if allowed { t.Fatal("missing Central snapshot must default discounts closed") }
	maxPercent, err := store.Float64(ctx, "billing.max_discount_percent", 20)
	if err != nil { t.Fatal(err) }
	if maxPercent != 20 { t.Fatalf("missing Central max discount should use V1 default 20, got %v", maxPercent) }

	if err := store.Save(ctx, Snapshot{
		SchemaVersion: 1,
		ETag: "discount-policy-1",
		Values: map[string]any{
			"billing.allow_discount": true,
			"billing.max_discount_percent": 12.5,
		},
	}); err != nil { t.Fatal(err) }

	allowed, err = store.Bool(ctx, "billing.allow_discount", false)
	if err != nil { t.Fatal(err) }
	if !allowed { t.Fatal("cached Central discount permission was not honored") }
	maxPercent, err = store.Float64(ctx, "billing.max_discount_percent", 20)
	if err != nil { t.Fatal(err) }
	if maxPercent != 12.5 { t.Fatalf("cached Central max discount mismatch: got %v", maxPercent) }
}

func TestV1CachedDiscountPolicyRejectsMalformedNumericLimit(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	store := NewStore(db)
	if err := store.Save(ctx, Snapshot{
		SchemaVersion: 1,
		ETag: "discount-policy-malformed",
		Values: map[string]any{"billing.max_discount_percent": "20"},
	}); err != nil { t.Fatal(err) }

	if _, err := store.Float64(ctx, "billing.max_discount_percent", 20); err == nil {
		t.Fatal("malformed Central numeric discount limit must fail closed")
	}
}
