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

func TestLineDiscountHonorsCentralPolicyOverLiveHTTP(t *testing.T) {
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
		VALUES(?,?,?,?,?,?,?,?)`, "discount-product-1", "Discount Item", "unit", 1, 0, 0, 1, "2026-08-30T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, "discount-price-1", "discount-product-1", "store-1", "INR", 10000, 1, 100, 1, "2026-08-30T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	catalogRepo := catalog.NewRepository(db)
	orderService := orders.New(db, catalogRepo)
	orderService.SetDiscountPolicy(func(context.Context) (orders.DiscountPolicy, error) {
		return orders.DiscountPolicy{Allowed: false, MaxPercent: 20}, nil
	})

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

	payload := func(clientOrderID string, discountMinor int64) []byte {
		raw, err := json.Marshal(map[string]any{
			"client_order_id": clientOrderID,
			"currency":        "INR",
			"items": []map[string]any{{
				"product_id": "discount-product-1", "quantity_milli": 1000, "discount_minor": discountMinor, "tax_minor": 0,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(payload("discount-denied-live", 1000)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		_ = resp.Body.Close()
		t.Fatalf("disabled discount status=%d want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
	var denied map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&denied); err != nil {
		_ = resp.Body.Close()
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if denied["error"] != "discount_not_allowed" {
		t.Fatalf("disabled discount error=%v", denied["error"])
	}

	orderService.SetDiscountPolicy(func(context.Context) (orders.DiscountPolicy, error) {
		return orders.DiscountPolicy{Allowed: true, MaxPercent: 20}, nil
	})
	resp, err = client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(payload("discount-over-limit-live", 2001)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		_ = resp.Body.Close()
		t.Fatalf("over-limit discount status=%d want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
	var overLimit map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&overLimit); err != nil {
		_ = resp.Body.Close()
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if overLimit["error"] != "discount_limit_exceeded" {
		t.Fatalf("over-limit discount error=%v", overLimit["error"])
	}

	var rejectedCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders`).Scan(&rejectedCount); err != nil {
		t.Fatal(err)
	}
	if rejectedCount != 0 {
		t.Fatalf("rejected discounts persisted %d order(s), want 0", rejectedCount)
	}

	resp, err = client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(payload("discount-at-limit-live", 2000)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		_ = resp.Body.Close()
		t.Fatalf("at-limit discount status=%d want %d", resp.StatusCode, http.StatusCreated)
	}
	var created orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		_ = resp.Body.Close()
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if created.DiscountMinor != 2000 || created.TotalMinor != 8000 {
		t.Fatalf("discounted order snapshot mismatch: %+v", created)
	}

	var orderCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders`).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 {
		t.Fatalf("accepted discount persisted %d orders, want 1", orderCount)
	}
	var persistedDiscount int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT discount_minor FROM sales_order_items WHERE order_id=?`, created.ID).Scan(&persistedDiscount); err != nil {
		t.Fatal(err)
	}
	if persistedDiscount != 2000 {
		t.Fatalf("persisted line discount=%d want 2000", persistedDiscount)
	}
}
