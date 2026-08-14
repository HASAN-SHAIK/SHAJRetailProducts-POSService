package inbox

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestV1ProductRemovalDeactivatesAndCleansLookupFacts(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "product-removal.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

	service := New(db)
	productPayload := json.RawMessage(`{"id":"101","name":"Fresh Milk","is_active":true,"allow_manual_price":true,"track_inventory":true,"version":100}`)
	if err := service.Apply(ctx, Message{ID:"product-101-v100", Type:"catalog.product.upsert", SchemaVersion:1, Source:"central", Payload:productPayload}); err != nil { t.Fatal(err) }
	barcodePayload := json.RawMessage(`{"barcode":"8901234567890","product_id":"101","barcode_type":"EAN","is_primary":true}`)
	if err := service.Apply(ctx, Message{ID:"barcode-101", Type:"catalog.barcode.upsert", SchemaVersion:1, Source:"central", Payload:barcodePayload}); err != nil { t.Fatal(err) }
	pricePayload := json.RawMessage(`{"id":"product:101:store:branch-1","product_id":"101","store_id":"branch-1","currency":"INR","amount_minor":6550,"tax_inclusive":true,"priority":100,"version":100}`)
	if err := service.Apply(ctx, Message{ID:"price-101", Type:"catalog.price.upsert", SchemaVersion:1, Source:"central", Payload:pricePayload}); err != nil { t.Fatal(err) }

	removePayload := json.RawMessage(`{"id":"101","version":200,"source_updated_at":"2026-08-14T06:32:00Z"}`)
	if err := service.Apply(ctx, Message{ID:"product-101-remove-v200", Type:"catalog.product.remove", SchemaVersion:1, Source:"central", Payload:removePayload}); err != nil { t.Fatal(err) }

	repo := catalog.NewRepository(db)
	if _, err := repo.GetByBarcode(ctx, "8901234567890", "branch-1"); err == nil { t.Fatal("removed product still resolves by barcode") }
	matches, err := repo.Search(ctx, "Fresh Milk", "branch-1", 20)
	if err != nil { t.Fatal(err) }
	if len(matches) != 0 { t.Fatalf("removed product still appears in search: %#v", matches) }
	removed, err := repo.GetProduct(ctx, "101", "branch-1")
	if err != nil { t.Fatal(err) }
	if removed.IsActive { t.Fatalf("removed product remained active: %#v", removed) }
	if len(removed.Barcodes) != 0 || removed.Price != nil { t.Fatalf("removed product retained lookup/price facts: %#v", removed) }

	var barcodeCount, priceCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE product_id='101'`).Scan(&barcodeCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_prices WHERE product_id='101'`).Scan(&priceCount); err != nil { t.Fatal(err) }
	if barcodeCount != 0 || priceCount != 0 { t.Fatalf("removal cleanup diverged: barcodes=%d prices=%d", barcodeCount, priceCount) }

	// Older cleanup must not erase a newer reactivation.
	newerProduct := json.RawMessage(`{"id":"101","name":"Fresh Milk Again","is_active":true,"allow_manual_price":true,"track_inventory":true,"version":300}`)
	if err := service.Apply(ctx, Message{ID:"product-101-v300", Type:"catalog.product.upsert", SchemaVersion:1, Source:"central", Payload:newerProduct}); err != nil { t.Fatal(err) }
	if err := service.Apply(ctx, Message{ID:"product-101-remove-v150", Type:"catalog.product.remove", SchemaVersion:1, Source:"central", Payload:json.RawMessage(`{"id":"101","version":150}`)}); err != nil { t.Fatal(err) }
	current, err := repo.GetProduct(ctx, "101", "branch-1")
	if err != nil { t.Fatal(err) }
	if !current.IsActive || current.Name != "Fresh Milk Again" { t.Fatalf("stale removal overrode newer product: %#v", current) }
}
