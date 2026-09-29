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

func TestManualPriceOverrideHonorsPolicyOverLiveHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`, "manual-product-1", "Manual Price Item", "unit", 1, 1, 0, 1, "2026-08-30T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, "manual-price-1", "manual-product-1", "store-1", "INR", 10000, 1, 100, 1, "2026-08-30T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	catalogRepo := catalog.NewRepository(db)
	orderService := orders.New(db, catalogRepo)
	orderService.SetPriceOverridePolicy(func(context.Context) (bool, error) { return false, nil })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	app := New(config.Config{Environment: "test", ListenAddress: addr}, db, deviceService, catalogRepo, customer.NewRepository(db), orderService, payments.New(db), inventory.New(db), receipts.New(db))
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
			t.Error("server did not stop")
		}
	})

	baseURL := "http://" + addr
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get(baseURL + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("POSService did not become healthy: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	payload := func(clientOrderID string) []byte {
		raw, err := json.Marshal(map[string]any{
			"client_order_id": clientOrderID,
			"currency":        "INR",
			"items": []map[string]any{{
				"product_id": "manual-product-1", "quantity_milli": 1000, "unit_price_minor": 9000, "discount_minor": 0, "tax_minor": 0,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(payload("manual-denied-live")))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		_ = resp.Body.Close()
		t.Fatalf("denied override status=%d want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
	var denied map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&denied); err != nil {
		_ = resp.Body.Close()
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if denied["error"] != "price_override_not_allowed" {
		t.Fatalf("denied override error=%v", denied["error"])
	}
	var orderCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders`).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 0 {
		t.Fatalf("denied override persisted %d order(s), want 0", orderCount)
	}

	orderService.SetPriceOverridePolicy(func(context.Context) (bool, error) { return true, nil })
	resp, err = client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(payload("manual-allowed-live")))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		_ = resp.Body.Close()
		t.Fatalf("allowed override status=%d want %d", resp.StatusCode, http.StatusCreated)
	}
	var created orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		_ = resp.Body.Close()
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if len(created.Items) != 1 || created.Items[0].UnitPriceMinor != 9000 || created.TotalMinor != 9000 {
		t.Fatalf("allowed override snapshot mismatch: %+v", created)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders`).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 {
		t.Fatalf("allowed override persisted %d orders, want 1", orderCount)
	}
	var persistedUnitPrice int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT unit_price_minor FROM sales_order_items WHERE order_id=?`, created.ID).Scan(&persistedUnitPrice); err != nil {
		t.Fatal(err)
	}
	if persistedUnitPrice != 9000 {
		t.Fatalf("persisted manual price=%d want 9000", persistedUnitPrice)
	}
}
