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
        "id": "101",
        "name": "GST Product",
        "tax_code": "0401",
        "gst_rate_percent": 18.0,
        "is_active": true,
        "allow_manual_price": false,
        "track_inventory": true,
        "version": 1,
    })
    if err != nil { t.Fatal(err) }

    if err := svc.Apply(ctx, Message{ID: "gst-product-101-v1", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central", Payload: payload}); err != nil {
        t.Fatalf("apply product projection: %v", err)
    }

    product, err := catalog.NewRepository(db).GetProduct(ctx, "101", "store-1")
    if err != nil { t.Fatalf("read projected product: %v", err) }
    if product.TaxCode == nil || *product.TaxCode != "0401" { t.Fatalf("tax code = %v, want 0401", product.TaxCode) }
    if product.GSTRateBps == nil || *product.GSTRateBps != 1800 { t.Fatalf("gst rate bps = %v, want 1800", product.GSTRateBps) }

    if err := svc.Apply(ctx, Message{ID: "gst-product-101-v1", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central", Payload: payload}); err != nil {
        t.Fatalf("replay product projection: %v", err)
    }
    replayed, err := catalog.NewRepository(db).GetProduct(ctx, "101", "store-1")
    if err != nil { t.Fatal(err) }
    if replayed.GSTRateBps == nil || *replayed.GSTRateBps != 1800 { t.Fatalf("replayed gst rate bps = %v", replayed.GSTRateBps) }
}

func TestV1ProductGSTProjectionRejectsInvalidRate(t *testing.T) {
    ctx := context.Background()
    db := testutil.OpenDatabase(t)
    svc := New(db)

    payload := json.RawMessage(`{"id":"bad-tax","name":"Bad Tax","gst_rate_percent":101,"is_active":true,"version":1}`)
    // json.RawMessage above is intentionally replaced with valid JSON below so the
    // assertion exercises GST-rate validation rather than generic JSON validation.
    payload = json.RawMessage(`{"id":"bad-tax"}`)
    payload, _ = json.Marshal(map[string]any{
        "id": "bad-tax",
        "name": "Bad Tax",
        "gst_rate_percent": 101,
        "is_active": true,
        "version": 1,
    })
    err := svc.Apply(ctx, Message{ID: "bad-tax-v1", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central", Payload: payload})
    if err == nil { t.Fatal("expected invalid GST rate to fail") }

    var count int
    if scanErr := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_products WHERE id='bad-tax'`).Scan(&count); scanErr != nil { t.Fatal(scanErr) }
    if count != 0 { t.Fatalf("invalid GST product persisted: count=%d", count) }
}
