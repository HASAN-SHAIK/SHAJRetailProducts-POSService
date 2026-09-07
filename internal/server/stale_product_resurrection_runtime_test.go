package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
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

func TestStaleProductUpsertCannotResurrectRemovedProductOverLiveHTTP(t *testing.T) {
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
		ID: "product-101-v200", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"101","name":"Fresh Milk","is_active":true,"allow_manual_price":true,"track_inventory":true,"version":200}`),
	}); err != nil {
		t.Fatalf("seed product projection: %v", err)
	}
	if err := projector.Apply(ctx, inbox.Message{
		ID: "barcode-101-v200", Type: "catalog.barcode.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"barcode":"8901234567890","product_id":"101","barcode_type":"EAN","is_primary":true}`),
	}); err != nil {
		t.Fatalf("seed barcode projection: %v", err)
	}
	if err := projector.Apply(ctx, inbox.Message{
		ID: "price-101-v200", Type: "catalog.price.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"product:101:store:branch-1","product_id":"101","store_id":"branch-1","currency":"INR","amount_minor":6550,"tax_inclusive":true,"priority":100,"version":200}`),
	}); err != nil {
		t.Fatalf("seed price projection: %v", err)
	}

	catalogRepo := catalog.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: freeStaleProductResurrectionAddress(t)},
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
	waitForStaleProductResurrectionHealth(t, client, baseURL)

	before, err := client.Get(baseURL + "/api/v1/catalog/products/barcode/8901234567890")
	if err != nil {
		t.Fatalf("pre-removal barcode lookup: %v", err)
	}
	_ = before.Body.Close()
	if before.StatusCode != http.StatusOK {
		t.Fatalf("pre-removal barcode status=%d", before.StatusCode)
	}

	if err := projector.Apply(ctx, inbox.Message{
		ID: "product-101-remove-v300", Type: "catalog.product.remove", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"101","version":300,"source_updated_at":"2026-08-30T18:30:00Z"}`),
	}); err != nil {
		t.Fatalf("apply product removal: %v", err)
	}

	removed := getStaleProductResurrectionProduct(t, client, baseURL)
	if removed.IsActive || removed.Name != "Fresh Milk" || len(removed.Barcodes) != 0 || removed.Price != nil {
		t.Fatalf("removal did not produce expected live catalog state: %+v", removed)
	}

	if err := projector.Apply(ctx, inbox.Message{
		ID: "product-101-stale-v250", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"101","name":"Stale Resurrected Milk","is_active":true,"allow_manual_price":true,"track_inventory":true,"version":250,"source_updated_at":"2026-08-30T18:20:00Z"}`),
	}); err != nil {
		t.Fatalf("apply stale product upsert: %v", err)
	}

	afterStale := getStaleProductResurrectionProduct(t, client, baseURL)
	if afterStale.IsActive {
		t.Fatalf("stale upsert resurrected removed product: %+v", afterStale)
	}
	if afterStale.Name != "Fresh Milk" {
		t.Fatalf("stale upsert replaced newer removed product facts: %+v", afterStale)
	}
	if len(afterStale.Barcodes) != 0 || afterStale.Price != nil {
		t.Fatalf("stale upsert restored removed lookup facts: %+v", afterStale)
	}

	barcodeAfterStale, err := client.Get(baseURL + "/api/v1/catalog/products/barcode/8901234567890")
	if err != nil {
		t.Fatalf("post-stale barcode lookup: %v", err)
	}
	_ = barcodeAfterStale.Body.Close()
	if barcodeAfterStale.StatusCode != http.StatusNotFound {
		t.Fatalf("stale upsert restored removed barcode: status=%d", barcodeAfterStale.StatusCode)
	}

	var name string
	var isActive, version, barcodeCount, priceCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT name,is_active,version FROM catalog_products WHERE id='101'`).Scan(&name, &isActive, &version); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE product_id='101'`).Scan(&barcodeCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_prices WHERE product_id='101'`).Scan(&priceCount); err != nil {
		t.Fatal(err)
	}
	if name != "Fresh Milk" || isActive != 0 || version != 300 || barcodeCount != 0 || priceCount != 0 {
		t.Fatalf("persisted stale-resurrection guard diverged: name=%q active=%d version=%d barcodes=%d prices=%d", name, isActive, version, barcodeCount, priceCount)
	}
}

func getStaleProductResurrectionProduct(t *testing.T, client *http.Client, baseURL string) catalog.Product {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/v1/catalog/products/101")
	if err != nil {
		t.Fatalf("product readback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("product readback status=%d", resp.StatusCode)
	}
	var product catalog.Product
	if err := json.NewDecoder(resp.Body).Decode(&product); err != nil {
		t.Fatalf("decode product readback: %v", err)
	}
	return product
}

func freeStaleProductResurrectionAddress(t *testing.T) string {
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

func waitForStaleProductResurrectionHealth(t *testing.T, client *http.Client, baseURL string) {
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
