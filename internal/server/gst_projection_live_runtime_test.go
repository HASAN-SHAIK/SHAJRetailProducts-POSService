package server

import (
	"bytes"
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

func TestCentralGSTProjectionDrivesLiveCatalogAndOrderTaxSnapshot(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{
		StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01",
	}); err != nil {
		t.Fatal(err)
	}

	projector := inbox.New(db)
	if err := projector.Apply(ctx, inbox.Message{
		ID: "gst-product-v100", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"gst-projected-1","name":"Projected GST Item","tax_code":"HSN-5","gst_rate_percent":5,"is_active":true,"allow_manual_price":false,"track_inventory":false,"version":100,"source_updated_at":"2026-08-30T18:00:00Z"}`),
	}); err != nil {
		t.Fatalf("project initial GST product: %v", err)
	}
	if err := projector.Apply(ctx, inbox.Message{
		ID: "gst-price-v100", Type: "catalog.price.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"gst-price-1","product_id":"gst-projected-1","store_id":"store-1","currency":"INR","amount_minor":10000,"tax_inclusive":false,"priority":100,"version":100,"source_updated_at":"2026-08-30T18:00:00Z"}`),
	}); err != nil {
		t.Fatalf("project GST price: %v", err)
	}

	catalogRepo := catalog.NewRepository(db)
	orderService := orders.New(db, catalogRepo)
	orderService.SetTaxPolicy(func(context.Context) (orders.TaxPolicy, error) {
		return orders.TaxPolicy{Enabled: true, Mode: "EXCLUSIVE", RoundingMode: "HALF_UP"}, nil
	})

	addr := freeGSTProjectionAddress(t)
	app := New(
		config.Config{Environment: "test", ListenAddress: addr},
		db,
		deviceService,
		catalogRepo,
		customer.NewRepository(db),
		orderService,
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

	baseURL := "http://" + addr
	client := &http.Client{Timeout: 2 * time.Second}
	waitForGSTProjectionHealth(t, client, baseURL)

	initialProduct := getGSTProjectionProduct(t, client, baseURL)
	if initialProduct.TaxCode == nil || *initialProduct.TaxCode != "HSN-5" || initialProduct.GSTRateBps == nil || *initialProduct.GSTRateBps != 500 {
		t.Fatalf("initial projected GST facts not visible over live catalog: %+v", initialProduct)
	}

	first := createGSTProjectionOrder(t, client, baseURL, "gst-projection-order-5")
	if first.TaxMinor != 500 || first.TotalMinor != 10500 || len(first.Items) != 1 {
		t.Fatalf("initial projected GST did not drive order totals: %+v", first)
	}
	if first.Items[0].TaxCode == nil || *first.Items[0].TaxCode != "HSN-5" || first.Items[0].GSTRateBps == nil || *first.Items[0].GSTRateBps != 500 || first.Items[0].TaxMinor != 500 {
		t.Fatalf("initial order GST snapshot mismatch: %+v", first.Items[0])
	}

	if err := projector.Apply(ctx, inbox.Message{
		ID: "gst-product-v200", Type: "catalog.product.upsert", SchemaVersion: 1, Source: "central",
		Payload: json.RawMessage(`{"id":"gst-projected-1","name":"Projected GST Item","tax_code":"HSN-18","gst_rate_percent":18,"is_active":true,"allow_manual_price":false,"track_inventory":false,"version":200,"source_updated_at":"2026-08-30T19:00:00Z"}`),
	}); err != nil {
		t.Fatalf("project updated GST product: %v", err)
	}

	updatedProduct := getGSTProjectionProduct(t, client, baseURL)
	if updatedProduct.TaxCode == nil || *updatedProduct.TaxCode != "HSN-18" || updatedProduct.GSTRateBps == nil || *updatedProduct.GSTRateBps != 1800 {
		t.Fatalf("updated projected GST facts not visible over live catalog: %+v", updatedProduct)
	}

	second := createGSTProjectionOrder(t, client, baseURL, "gst-projection-order-18")
	if second.TaxMinor != 1800 || second.TotalMinor != 11800 || len(second.Items) != 1 {
		t.Fatalf("updated projected GST did not drive order totals: %+v", second)
	}
	if second.Items[0].TaxCode == nil || *second.Items[0].TaxCode != "HSN-18" || second.Items[0].GSTRateBps == nil || *second.Items[0].GSTRateBps != 1800 || second.Items[0].TaxMinor != 1800 {
		t.Fatalf("updated order GST snapshot mismatch: %+v", second.Items[0])
	}

	var firstCode, secondCode string
	var firstRate, firstTax, secondRate, secondTax int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT tax_code,gst_rate_bps,tax_minor FROM sales_order_items WHERE order_id=?`, first.ID).Scan(&firstCode, &firstRate, &firstTax); err != nil {
		t.Fatalf("read first persisted GST snapshot: %v", err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT tax_code,gst_rate_bps,tax_minor FROM sales_order_items WHERE order_id=?`, second.ID).Scan(&secondCode, &secondRate, &secondTax); err != nil {
		t.Fatalf("read second persisted GST snapshot: %v", err)
	}
	if firstCode != "HSN-5" || firstRate != 500 || firstTax != 500 {
		t.Fatalf("first persisted GST snapshot changed after Central update: code=%q rate=%d tax=%d", firstCode, firstRate, firstTax)
	}
	if secondCode != "HSN-18" || secondRate != 1800 || secondTax != 1800 {
		t.Fatalf("second persisted GST snapshot mismatch: code=%q rate=%d tax=%d", secondCode, secondRate, secondTax)
	}
}

func createGSTProjectionOrder(t *testing.T, client *http.Client, baseURL, clientOrderID string) orders.Order {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"client_order_id": clientOrderID,
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "gst-projected-1", "quantity_milli": 1000,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("create %s: %v", clientOrderID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create %s status=%d want=%d", clientOrderID, resp.StatusCode, http.StatusCreated)
	}
	var created orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode %s: %v", clientOrderID, err)
	}
	return created
}

func getGSTProjectionProduct(t *testing.T, client *http.Client, baseURL string) catalog.Product {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/v1/catalog/products/gst-projected-1")
	if err != nil {
		t.Fatalf("live GST product readback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("live GST product readback status=%d", resp.StatusCode)
	}
	var product catalog.Product
	if err := json.NewDecoder(resp.Body).Decode(&product); err != nil {
		t.Fatalf("decode live GST product: %v", err)
	}
	return product
}

func freeGSTProjectionAddress(t *testing.T) string {
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

func waitForGSTProjectionHealth(t *testing.T, client *http.Client, baseURL string) {
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
