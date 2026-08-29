package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
)

func TestV1CatalogRuntimeHTTPSearchBarcodeProductAndCategories(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, filepath.Join(t.TempDir(), "pos.db"))
	defer db.Close()

	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil {
		t.Fatal(err)
	}

	seedRuntimeCatalog(t, db)
	app := newTestServer(db, deviceService)

	search := serveJSON(t, app, http.MethodGet, "/api/v1/catalog/products?q=milk&limit=10", nil)
	if search.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}
	var searchBody struct {
		Items []catalog.Product `json:"items"`
		Count int               `json:"count"`
	}
	if err := json.NewDecoder(search.Body).Decode(&searchBody); err != nil {
		t.Fatal(err)
	}
	if searchBody.Count != 1 || len(searchBody.Items) != 1 {
		t.Fatalf("unexpected search result: %#v", searchBody)
	}
	assertRuntimeProduct(t, searchBody.Items[0])

	barcode := serveJSON(t, app, http.MethodGet, "/api/v1/catalog/products/barcode/8901234567890", nil)
	if barcode.Code != http.StatusOK {
		t.Fatalf("barcode status=%d body=%s", barcode.Code, barcode.Body.String())
	}
	var barcodeProduct catalog.Product
	if err := json.NewDecoder(barcode.Body).Decode(&barcodeProduct); err != nil {
		t.Fatal(err)
	}
	assertRuntimeProduct(t, barcodeProduct)

	product := serveJSON(t, app, http.MethodGet, "/api/v1/catalog/products/product-milk", nil)
	if product.Code != http.StatusOK {
		t.Fatalf("product status=%d body=%s", product.Code, product.Body.String())
	}
	var byID catalog.Product
	if err := json.NewDecoder(product.Body).Decode(&byID); err != nil {
		t.Fatal(err)
	}
	assertRuntimeProduct(t, byID)

	categories := serveJSON(t, app, http.MethodGet, "/api/v1/catalog/categories", nil)
	if categories.Code != http.StatusOK {
		t.Fatalf("categories status=%d body=%s", categories.Code, categories.Body.String())
	}
	var categoryBody struct {
		Items []catalog.Category `json:"items"`
		Count int                `json:"count"`
	}
	if err := json.NewDecoder(categories.Body).Decode(&categoryBody); err != nil {
		t.Fatal(err)
	}
	if categoryBody.Count != 1 || len(categoryBody.Items) != 1 || categoryBody.Items[0].ID != "category-dairy" {
		t.Fatalf("unexpected categories: %#v", categoryBody)
	}

	inactiveSearch := serveJSON(t, app, http.MethodGet, "/api/v1/catalog/products?q=retired", nil)
	if inactiveSearch.Code != http.StatusOK {
		t.Fatalf("inactive search status=%d body=%s", inactiveSearch.Code, inactiveSearch.Body.String())
	}
	var inactiveBody struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(inactiveSearch.Body).Decode(&inactiveBody); err != nil {
		t.Fatal(err)
	}
	if inactiveBody.Count != 0 {
		t.Fatalf("inactive product leaked into cashier search: %#v", inactiveBody)
	}

	missingBarcode := serveJSON(t, app, http.MethodGet, "/api/v1/catalog/products/barcode/0000000000000", nil)
	if missingBarcode.Code != http.StatusNotFound {
		t.Fatalf("missing barcode status=%d body=%s", missingBarcode.Code, missingBarcode.Body.String())
	}
}

func seedRuntimeCatalog(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO catalog_categories(id,name,sort_order,is_active,version,updated_at) VALUES('category-dairy','Dairy',1,1,1,'2026-01-01T00:00:00Z')`,
		`INSERT INTO catalog_products(id,category_id,sku,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES('product-milk','category-dairy','MILK-500','Amul Milk 500ml','unit',1,0,1,1,'2026-01-01T00:00:00Z')`,
		`INSERT INTO catalog_barcodes(barcode,product_id,is_primary,updated_at) VALUES('8901234567890','product-milk',1,'2026-01-01T00:00:00Z')`,
		`INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES('price-milk','product-milk','store-1','INR',3200,1,100,1,'2026-01-01T00:00:00Z')`,
		`INSERT INTO catalog_products(id,sku,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES('product-retired','OLD-1','Retired Product','unit',0,0,1,1,'2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed catalog: %v", err)
		}
	}
}

func assertRuntimeProduct(t *testing.T, product catalog.Product) {
	t.Helper()
	if product.ID != "product-milk" || product.Name != "Amul Milk 500ml" || !product.IsActive {
		t.Fatalf("unexpected product: %#v", product)
	}
	if len(product.Barcodes) != 1 || product.Barcodes[0] != "8901234567890" {
		t.Fatalf("unexpected barcodes: %#v", product.Barcodes)
	}
	if product.Price == nil || product.Price.Currency != "INR" || product.Price.AmountMinor != 3200 || !product.Price.TaxInclusive {
		t.Fatalf("unexpected store price: %#v", product.Price)
	}
}
