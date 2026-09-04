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
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestSaleTimeProductIdentitySnapshotSurvivesCatalogRenameOverLiveHTTP(t *testing.T) {
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

	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(id,sku,name,is_active,allow_manual_price,track_inventory,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		"101", "COLA-OLD", "Classic Cola", 1, 0, 0, 1, "2026-08-31T00:00:00Z",
	); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"price-101", "101", "store-1", "INR", 7500, 1, 100, 1, "2026-08-31T00:00:00Z",
	); err != nil {
		t.Fatalf("insert price: %v", err)
	}

	catalogRepo := catalog.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: freeProductIdentitySnapshotAddress(t)},
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
	waitForProductIdentitySnapshotHealth(t, client, baseURL)

	payload := map[string]any{
		"client_order_id": "runtime-product-identity-snapshot-order",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "101", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0,
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("create order request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create order status=%d body=%v", resp.StatusCode, body)
	}
	var created orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created order: %v", err)
	}
	if len(created.Items) != 1 {
		t.Fatalf("expected one order item, got %+v", created.Items)
	}
	if created.Items[0].ProductName != "Classic Cola" {
		t.Fatalf("live create lost product-name snapshot: %+v", created.Items[0])
	}
	if created.Items[0].SKU == nil || *created.Items[0].SKU != "COLA-OLD" {
		t.Fatalf("live create lost sku snapshot: %+v", created.Items[0])
	}

	if _, err := db.SQL().ExecContext(ctx, `
		UPDATE catalog_products SET name=?, sku=?, version=version+1, updated_at=? WHERE id=?`,
		"Cola Zero", "COLA-NEW", "2026-08-31T00:05:00Z", "101",
	); err != nil {
		t.Fatalf("rename current product: %v", err)
	}

	catalogResp, err := client.Get(baseURL + "/api/v1/catalog/products/101")
	if err != nil {
		t.Fatalf("catalog readback request: %v", err)
	}
	defer catalogResp.Body.Close()
	if catalogResp.StatusCode != http.StatusOK {
		var body any
		_ = json.NewDecoder(catalogResp.Body).Decode(&body)
		t.Fatalf("catalog readback status=%d body=%v", catalogResp.StatusCode, body)
	}
	var current catalog.Product
	if err := json.NewDecoder(catalogResp.Body).Decode(&current); err != nil {
		t.Fatalf("decode catalog readback: %v", err)
	}
	if current.Name != "Cola Zero" || current.SKU == nil || *current.SKU != "COLA-NEW" {
		t.Fatalf("live catalog did not expose renamed product identity: %+v", current)
	}

	getResp, err := client.Get(baseURL + "/api/v1/orders/" + created.ID)
	if err != nil {
		t.Fatalf("reload order request: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		var body any
		_ = json.NewDecoder(getResp.Body).Decode(&body)
		t.Fatalf("reload order status=%d body=%v", getResp.StatusCode, body)
	}
	var reloaded orders.Order
	if err := json.NewDecoder(getResp.Body).Decode(&reloaded); err != nil {
		t.Fatalf("decode reloaded order: %v", err)
	}
	if len(reloaded.Items) != 1 || reloaded.Items[0].ProductName != "Classic Cola" {
		t.Fatalf("catalog rename rewrote historical product-name snapshot: %+v", reloaded.Items)
	}
	if reloaded.Items[0].SKU == nil || *reloaded.Items[0].SKU != "COLA-OLD" {
		t.Fatalf("catalog rename rewrote historical sku snapshot: %+v", reloaded.Items[0])
	}

	var persistedName string
	var persistedSKU *string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT product_name, sku FROM sales_order_items WHERE order_id=?`, created.ID,
	).Scan(&persistedName, &persistedSKU); err != nil {
		t.Fatalf("read persisted product identity snapshot: %v", err)
	}
	if persistedName != "Classic Cola" || persistedSKU == nil || *persistedSKU != "COLA-OLD" {
		t.Fatalf("persisted product identity snapshot mutated: name=%q sku=%v", persistedName, persistedSKU)
	}
}

func freeProductIdentitySnapshotAddress(t *testing.T) string {
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

func waitForProductIdentitySnapshotHealth(t *testing.T, client *http.Client, baseURL string) {
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
