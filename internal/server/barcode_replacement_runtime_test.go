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

func TestPrimaryBarcodeReplacementUpdatesLiveCatalogHTTP(t *testing.T) {
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
		ID: "product-101-v1", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"101","name":"Fresh Milk","is_active":true,"allow_manual_price":true,"track_inventory":true,"version":1}`),
	}); err != nil {
		t.Fatalf("seed product projection: %v", err)
	}
	if err := projector.Apply(ctx, inbox.Message{
		ID: "barcode-101-old", Type: "catalog.barcode.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"barcode":"8901234567890","product_id":"101","barcode_type":"EAN","is_primary":true}`),
	}); err != nil {
		t.Fatalf("seed old barcode projection: %v", err)
	}
	if err := projector.Apply(ctx, inbox.Message{
		ID: "price-101", Type: "catalog.price.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"product:101:store:branch-1","product_id":"101","store_id":"branch-1","currency":"INR","amount_minor":6550,"tax_inclusive":true,"priority":100,"version":1}`),
	}); err != nil {
		t.Fatalf("seed price projection: %v", err)
	}

	catalogRepo := catalog.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: freeBarcodeReplacementAddress(t)},
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
	waitForBarcodeReplacementHealth(t, client, baseURL)

	oldBefore, err := client.Get(baseURL + "/api/v1/catalog/products/barcode/8901234567890")
	if err != nil {
		t.Fatalf("pre-replacement old barcode lookup: %v", err)
	}
	_ = oldBefore.Body.Close()
	if oldBefore.StatusCode != http.StatusOK {
		t.Fatalf("pre-replacement old barcode status=%d", oldBefore.StatusCode)
	}

	newBarcode := inbox.Message{
		ID: "barcode-101-new", Type: "catalog.barcode.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"barcode":"8901234567891","product_id":"101","barcode_type":"EAN","is_primary":true}`),
	}
	if err := projector.Apply(ctx, newBarcode); err != nil {
		t.Fatalf("apply replacement barcode projection: %v", err)
	}

	oldAfter, err := client.Get(baseURL + "/api/v1/catalog/products/barcode/8901234567890")
	if err != nil {
		t.Fatalf("post-replacement old barcode lookup: %v", err)
	}
	_ = oldAfter.Body.Close()
	if oldAfter.StatusCode != http.StatusNotFound {
		t.Fatalf("stale primary barcode still resolves: status=%d", oldAfter.StatusCode)
	}

	newAfter, err := client.Get(baseURL + "/api/v1/catalog/products/barcode/8901234567891")
	if err != nil {
		t.Fatalf("post-replacement new barcode lookup: %v", err)
	}
	if newAfter.StatusCode != http.StatusOK {
		_ = newAfter.Body.Close()
		t.Fatalf("replacement barcode status=%d", newAfter.StatusCode)
	}
	var resolved catalog.Product
	if err := json.NewDecoder(newAfter.Body).Decode(&resolved); err != nil {
		_ = newAfter.Body.Close()
		t.Fatalf("decode replacement barcode lookup: %v", err)
	}
	_ = newAfter.Body.Close()
	if resolved.ID != "101" || resolved.Name != "Fresh Milk" {
		t.Fatalf("replacement barcode resolved wrong product: %+v", resolved)
	}

	productResp, err := client.Get(baseURL + "/api/v1/catalog/products/101")
	if err != nil {
		t.Fatalf("product readback: %v", err)
	}
	if productResp.StatusCode != http.StatusOK {
		_ = productResp.Body.Close()
		t.Fatalf("product readback status=%d", productResp.StatusCode)
	}
	var product catalog.Product
	if err := json.NewDecoder(productResp.Body).Decode(&product); err != nil {
		_ = productResp.Body.Close()
		t.Fatalf("decode product readback: %v", err)
	}
	_ = productResp.Body.Close()
	if len(product.Barcodes) != 1 || product.Barcodes[0].Barcode != "8901234567891" || !product.Barcodes[0].IsPrimary {
		t.Fatalf("live product retained stale/invalid primary barcode set: %+v", product.Barcodes)
	}

	var oldCount, newCount, totalPrimary int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE barcode='8901234567890'`).Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE barcode='8901234567891' AND product_id='101' AND is_primary=1`).Scan(&newCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE product_id='101' AND is_primary=1`).Scan(&totalPrimary); err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 || newCount != 1 || totalPrimary != 1 {
		t.Fatalf("persisted barcode replacement diverged: old=%d new=%d primary=%d", oldCount, newCount, totalPrimary)
	}

	if err := projector.Apply(ctx, newBarcode); err != nil {
		t.Fatalf("duplicate replacement replay: %v", err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_barcodes WHERE product_id='101' AND is_primary=1`).Scan(&totalPrimary); err != nil {
		t.Fatal(err)
	}
	if totalPrimary != 1 {
		t.Fatalf("duplicate replay changed primary barcode cardinality: %d", totalPrimary)
	}
}

func freeBarcodeReplacementAddress(t *testing.T) string {
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

func waitForBarcodeReplacementHealth(t *testing.T, client *http.Client, baseURL string) {
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
