package effectiveconfig

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1CachedManualPricePolicyDefaultsClosedAndPersistsCentralValue(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	store := NewStore(db)

	allowed, err := store.Bool(ctx, "billing.allow_price_override", false)
	if err != nil { t.Fatal(err) }
	if allowed { t.Fatal("missing Central snapshot must default price override closed") }

	if err := store.Save(ctx, Snapshot{
		SchemaVersion: 1,
		ETag: "pricing-policy-1",
		Values: map[string]any{"billing.allow_price_override": true},
		Config: map[string]any{"billing": map[string]any{"allow_price_override": true}},
	}); err != nil { t.Fatal(err) }

	allowed, err = store.Bool(ctx, "billing.allow_price_override", false)
	if err != nil { t.Fatal(err) }
	if !allowed { t.Fatal("cached Central price override policy was not honored") }
}

func TestV1CachedManualPricePolicyRejectsMalformedCentralValue(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	store := NewStore(db)
	if err := store.Save(ctx, Snapshot{
		SchemaVersion: 1,
		ETag: "pricing-policy-malformed",
		Values: map[string]any{"billing.allow_price_override": "true"},
	}); err != nil { t.Fatal(err) }

	if _, err := store.Bool(ctx, "billing.allow_price_override", false); err == nil {
		t.Fatal("malformed Central boolean policy must fail closed")
	}
}
