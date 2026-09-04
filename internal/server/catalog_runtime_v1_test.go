package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
)

type catalogRuntimeResponse struct {
	Status int
	Body   []byte
}

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
	baseURL := startCatalogLiveRuntime(t, app)

	search := catalogRuntimeGet(t, baseURL+"/api/v1/catalog/products?q=milk&limit=10")
	if search.Status != http.StatusOK {
		t.Fatalf("search status=%d body=%s", search.Status, search.Body)
	}
	var searchBody struct {
		Items []catalog.Product `json:"items"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(search.Body, &searchBody); err != nil {
		t.Fatal(err)
	}
	if searchBody.Count != 1 || len(searchBody.Items) != 1 {
		t.Fatalf("unexpected search result: %#v", searchBody)
	}
	assertRuntimeProduct(t, searchBody.Items[0])

	barcode := catalogRuntimeGet(t, baseURL+"/api/v1/catalog/products/barcode/8901234567890")
	if barcode.Status != http.StatusOK {
		t.Fatalf("barcode status=%d body=%s", barcode.Status, barcode.Body)
	}
	var barcodeProduct catalog.Product
	if err := json.Unmarshal(barcode.Body, &barcodeProduct); err != nil {
		t.Fatal(err)
	}
	assertRuntimeProduct(t, barcodeProduct)

	product := catalogRuntimeGet(t, baseURL+"/api/v1/catalog/products/product-milk")
	if product.Status != http.StatusOK {
		t.Fatalf("product status=%d body=%s", product.Status, product.Body)
	}
	var byID catalog.Product
	if err := json.Unmarshal(product.Body, &byID); err != nil {
		t.Fatal(err)
	}
	assertRuntimeProduct(t, byID)

	categories := catalogRuntimeGet(t, baseURL+"/api/v1/catalog/categories")
	if categories.Status != http.StatusOK {
		t.Fatalf("categories status=%d body=%s", categories.Status, categories.Body)
	}
	var categoryBody struct {
		Items []catalog.Category `json:"items"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(categories.Body, &categoryBody); err != nil {
		t.Fatal(err)
	}
	if categoryBody.Count != 1 || len(categoryBody.Items) != 1 || categoryBody.Items[0].ID != "category-dairy" {
		t.Fatalf("unexpected categories: %#v", categoryBody)
	}

	inactiveSearch := catalogRuntimeGet(t, baseURL+"/api/v1/catalog/products?q=retired")
	if inactiveSearch.Status != http.StatusOK {
		t.Fatalf("inactive search status=%d body=%s", inactiveSearch.Status, inactiveSearch.Body)
	}
	var inactiveBody struct { Count int `json:"count"` }
	if err := json.Unmarshal(inactiveSearch.Body, &inactiveBody); err != nil {
		t.Fatal(err)
	}
	if inactiveBody.Count != 0 {
		t.Fatalf("inactive product leaked into cashier search: %#v", inactiveBody)
	}

	missingBarcode := catalogRuntimeGet(t, baseURL+"/api/v1/catalog/products/barcode/0000000000000")
	if missingBarcode.Status != http.StatusNotFound {
		t.Fatalf("missing barcode status=%d body=%s", missingBarcode.Status, missingBarcode.Body)
	}
}

func startCatalogLiveRuntime(t *testing.T, app *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil { t.Fatal(err) }
	app.cfg.ListenAddress = addr
	app.httpServer.Addr = addr
	serverErr := make(chan error, 1)
	go func() { serverErr <- app.Start() }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(shutdownCtx)
		select {
		case err := <-serverErr:
			if err != nil { t.Errorf("catalog runtime shutdown: %v", err) }
		case <-time.After(2 * time.Second):
			t.Error("catalog runtime did not stop")
		}
	})

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get("http://" + addr + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK { return "http://" + addr }
		}
		if time.Now().After(deadline) { t.Fatalf("catalog POSService runtime did not become healthy: %v", err) }
		time.Sleep(25 * time.Millisecond)
	}
}

func catalogRuntimeGet(t *testing.T, url string) catalogRuntimeResponse {
	t.Helper()
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(url)
	if err != nil { t.Fatalf("runtime GET %s: %v", url, err) }
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil { t.Fatal(err) }
	return catalogRuntimeResponse{Status: resp.StatusCode, Body: raw}
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
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil { t.Fatalf("seed catalog: %v", err) }
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
