package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestProductRemovalProjectionDisappearsFromLiveCatalogHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{
		StoreID: "branch-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01",
	}); err != nil {
		t.Fatal(err)
	}

	projector := inbox.New(db)
	if err := projector.Apply(ctx, inbox.Message{
		ID: "product-101-v100", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"101","name":"Fresh Milk","is_active":true,"allow_manual_price":true,"track_inventory":true,"version":100}`),
	}); err != nil {
		t.Fatalf("seed product projection: %v", err)
	}
	if err := projector.Apply(ctx, inbox.Message{
		ID: "barcode-101", Type: "catalog.barcode.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"barcode":"8901234567890","product_id":"101","barcode_type":"EAN","is_primary":true}`),
	}); err != nil {
		t.Fatalf("seed barcode projection: %v", err)
	}
	if err := projector.Apply(ctx, inbox.Message{
		ID: "price-101", Type: "catalog.price.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"product:101:store:branch-1","product_id":"101","store_id":"branch-1","currency":"INR","amount_minor":6550,"tax_inclusive":true,"priority":100,"version":100}`),
	}); err != nil {
		t.Fatalf("seed price projection: %v", err)
	}

	catalogRepo := catalog.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: freeProductRemovalAddress(t)},
		db,
		deviceService,
		catalogRepo,
		customer.NewRepository(db),
		orders.New(db, catalogRepo),
		payments.New(db),
		inventory.New(db),
		receipts.New(db),
	)

	serverErr := make(chan error, 1)
	go func() { serverErr <- app.Start() }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(shutdownCtx)
		select {
		case err := <-serverErr:
			if err != nil {
				t.Errorf("server shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("server did not stop after shutdown")
		}
	})

	baseURL := "http://" + app.cfg.ListenAddress
	client := &http.Client{Timeout: 2 * time.Second}
	waitForProductRemovalHealth(t, client, baseURL)

	beforeBarcode, err := client.Get(baseURL + "/api/v1/catalog/products/barcode/8901234567890")
	if err != nil {
		t.Fatalf("pre-removal barcode lookup: %v", err)
	}
	_ = beforeBarcode.Body.Close()
	if beforeBarcode.StatusCode != http.StatusOK {
		t.Fatalf("pre-removal barcode status=%d", beforeBarcode.StatusCode)
	}

	beforeSearch, err := client.Get(baseURL + "/api/v1/catalog/products?q=" + url.QueryEscape("Fresh Milk"))
	if err != nil {
		t.Fatalf("pre-removal search: %v", err)
	}
	var before struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(beforeSearch.Body).Decode(&before); err != nil {
		_ = beforeSearch.Body.Close()
		t.Fatalf("decode pre-removal search: %v", err)
	}
	_ = beforeSearch.Body.Close()
	if beforeSearch.StatusCode != http.StatusOK || before.Count != 1 {
		t.Fatalf("pre-removal search status=%d count=%d", beforeSearch.StatusCode, before.Count)
	}

	if err := projector.Apply(ctx, inbox.Message{
		ID: "product-101-remove-v200", Type: "catalog.product.remove", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"101","version":200,"source_updated_at":"2026-08-30T16:00:00Z"}`),
	}); err != nil {
		t.Fatalf("apply product removal projection: %v", err)
	}

	afterBarcode, err := client.Get(baseURL + "/api/v1/catalog/products/barcode/8901234567890")
	if err != nil {
		t.Fatalf("post-removal barcode lookup: %v", err)
	}
	_ = afterBarcode.Body.Close()
	if afterBarcode.StatusCode != http.StatusNotFound {
		t.Fatalf("removed barcode still resolves: status=%d", afterBarcode.StatusCode)
	}

	afterSearch, err := client.Get(baseURL + "/api/v1/catalog/products?q=" + url.QueryEscape("Fresh Milk"))
	if err != nil {
		t.Fatalf("post-removal search: %v", err)
	}
	var after struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(afterSearch.Body).Decode(&after); err != nil {
		_ = afterSearch.Body.Close()
		t.Fatalf("decode post-removal search: %v", err)
	}
	_ = afterSearch.Body.Close()
	if afterSearch.StatusCode != http.StatusOK || after.Count != 0 {
		t.Fatalf("removed product still searchable: status=%d count=%d", afterSearch.StatusCode, after.Count)
	}

	productResp, err := client.Get(baseURL + "/api/v1/catalog/products/101")
	if err != nil {
		t.Fatalf("post-removal product lookup: %v", err)
	}
	defer productResp.Body.Close()
	if productResp.StatusCode != http.StatusOK {
		t.Fatalf("post-removal product status=%d", productResp.StatusCode)
	}
	var removed catalog.Product
	if err := json.NewDecoder(productResp.Body).Decode(&removed); err != nil {
		t.Fatalf("decode removed product: %v", err)
	}
	if removed.IsActive || len(removed.Barcodes) != 0 || removed.Price != nil {
		t.Fatalf("removed product retained live lookup facts: %+v", removed)
	}

	var barcodeCount, priceCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE product_id='101'`).Scan(&barcodeCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_prices WHERE product_id='101'`).Scan(&priceCount); err != nil {
		t.Fatal(err)
	}
	if barcodeCount != 0 || priceCount != 0 {
		t.Fatalf("persisted removal cleanup diverged: barcodes=%d prices=%d", barcodeCount, priceCount)
	}
}

func freeProductRemovalAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitForProductRemovalHealth(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get(baseURL + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("live POSService did not become healthy at %s: %v", baseURL, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
