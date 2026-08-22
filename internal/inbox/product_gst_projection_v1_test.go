package inbox

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1ProductGSTProjectionPersistsCanonicalTaxFacts(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	svc := New(db)

	payload, err := json.Marshal(map[string]any{
		"id":                 "101",
		"name":               "GST Product",
		"company":            "GST Co",
		"tax_code":           "0401",
		"gst_rate_percent":   18.0,
		"mrp":                123.45,
		"expiry_date":        "2026-09-30",
		"is_weight_based":    true,
		"is_active":          true,
		"allow_manual_price": false,
		"track_inventory":    true,
		"version":            1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Apply(ctx, Message{ID: "gst-product-101-v1", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central", Payload: payload}); err != nil {
		t.Fatalf("apply product projection: %v", err)
	}

	product, err := catalog.NewRepository(db).GetProduct(ctx, "101", "store-1")
	if err != nil {
		t.Fatalf("read projected product: %v", err)
	}
	if product.TaxCode == nil || *product.TaxCode != "0401" {
		t.Fatalf("tax code = %v, want 0401", product.TaxCode)
	}
	if product.GSTRateBps == nil || *product.GSTRateBps != 1800 {
		t.Fatalf("gst rate bps = %v, want 1800", product.GSTRateBps)
	}
	if product.Company == nil || *product.Company != "GST Co" {
		t.Fatalf("company = %v, want GST Co", product.Company)
	}
	if product.MRP == nil || *product.MRP != 123.45 {
		t.Fatalf("mrp = %v, want 123.45", product.MRP)
	}
	if product.ExpiryDate == nil || *product.ExpiryDate != "2026-09-30" {
		t.Fatalf("expiry date = %v, want 2026-09-30", product.ExpiryDate)
	}
	if !product.IsWeightBased {
		t.Fatalf("is weight based = false, want true")
	}

	if err := svc.Apply(ctx, Message{ID: "gst-product-101-v1", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central", Payload: payload}); err != nil {
		t.Fatalf("replay product projection: %v", err)
	}
	replayed, err := catalog.NewRepository(db).GetProduct(ctx, "101", "store-1")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.GSTRateBps == nil || *replayed.GSTRateBps != 1800 {
		t.Fatalf("replayed gst rate bps = %v", replayed.GSTRateBps)
	}
}

func TestV1ProductProjectionSeedsInventoryBalance(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	svc := New(db)

	payload, err := json.Marshal(map[string]any{
		"id":                 "stock-101",
		"name":               "Stock Product",
		"is_active":          true,
		"allow_manual_price": false,
		"track_inventory":    true,
		"store_id":           "store-1",
		"stock_quantity":     10,
		"version":            100,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Apply(ctx, Message{ID: "stock-product-101-v1", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central", Payload: payload}); err != nil {
		t.Fatalf("apply product stock projection: %v", err)
	}

	var onHandMilli int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='stock-101'`).Scan(&onHandMilli); err != nil {
		t.Fatal(err)
	}
	if onHandMilli != 10000 {
		t.Fatalf("on_hand_milli=%d want 10000", onHandMilli)
	}
}

func TestV1ProductGSTProjectionRejectsInvalidRate(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	svc := New(db)

	payload, err := json.Marshal(map[string]any{
		"id":               "bad-tax",
		"name":             "Bad Tax",
		"gst_rate_percent": 101,
		"is_active":        true,
		"version":          1,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.Apply(ctx, Message{ID: "bad-tax-v1", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central", Payload: payload})
	if err == nil {
		t.Fatal("expected invalid GST rate to fail")
	}

	var count int
	if scanErr := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_products WHERE id='bad-tax'`).Scan(&count); scanErr != nil {
		t.Fatal(scanErr)
	}
	if count != 0 {
		t.Fatalf("invalid GST product persisted: count=%d", count)
	}
}
