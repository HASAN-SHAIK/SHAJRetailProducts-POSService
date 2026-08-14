package inbox

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestV1PrimaryBarcodeReplacementRemovesStalePrimary(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "barcode-replacement.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

	service := New(db)
	productPayload := json.RawMessage(`{"id":"101","name":"Fresh Milk","is_active":true,"allow_manual_price":true,"track_inventory":true,"version":1}`)
	if err := service.Apply(ctx, Message{ID:"product-101-v1", Type:"catalog.product.upsert", SchemaVersion:1, Source:"central", Payload:productPayload}); err != nil { t.Fatal(err) }

	oldBarcode := json.RawMessage(`{"barcode":"8901234567890","product_id":"101","barcode_type":"EAN","is_primary":true}`)
	if err := service.Apply(ctx, Message{ID:"barcode-101-old", Type:"catalog.barcode.upsert", SchemaVersion:1, Source:"central", Payload:oldBarcode}); err != nil { t.Fatal(err) }

	newBarcode := json.RawMessage(`{"barcode":"8901234567891","product_id":"101","barcode_type":"EAN","is_primary":true}`)
	if err := service.Apply(ctx, Message{ID:"barcode-101-new", Type:"catalog.barcode.upsert", SchemaVersion:1, Source:"central", Payload:newBarcode}); err != nil { t.Fatal(err) }

	var oldCount, newCount, totalPrimary int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE barcode='8901234567890'`).Scan(&oldCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE barcode='8901234567891' AND product_id='101' AND is_primary=1`).Scan(&newCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE product_id='101' AND is_primary=1`).Scan(&totalPrimary); err != nil { t.Fatal(err) }
	if oldCount != 0 || newCount != 1 || totalPrimary != 1 {
		t.Fatalf("primary barcode replacement diverged: old=%d new=%d primary=%d", oldCount, newCount, totalPrimary)
	}

	if err := service.Apply(ctx, Message{ID:"barcode-101-new", Type:"catalog.barcode.upsert", SchemaVersion:1, Source:"central", Payload:newBarcode}); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE product_id='101' AND is_primary=1`).Scan(&totalPrimary); err != nil { t.Fatal(err) }
	if totalPrimary != 1 { t.Fatalf("duplicate replay changed primary barcode cardinality: %d", totalPrimary) }
}
