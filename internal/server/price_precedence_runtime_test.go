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

func TestEffectivePricePrecedenceOverLiveHTTPServer(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,sku,name,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		"price-precedence-item", "PRICE-001", "Price Precedence Item", 1, 0, 0, 1, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	insertPrice := func(id string, store any, amount, priority int64, validFrom, validTo any, updatedAt time.Time) {
		t.Helper()
		if _, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,valid_from,valid_to,priority,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			id, "price-precedence-item", store, "INR", amount, 1, validFrom, validTo, priority, 1, updatedAt.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	insertPrice("global-high", nil, 12000, 999, nil, nil, now.Add(-time.Minute))
	insertPrice("store-low", "store-1", 9000, 10, nil, nil, now.Add(-2*time.Minute))
	insertPrice("store-high", "store-1", 9500, 20, nil, nil, now.Add(-3*time.Minute))
	insertPrice("store-expired", "store-1", 1000, 1000, now.Add(-2*time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano), now)
	insertPrice("store-future", "store-1", 2000, 2000, now.Add(time.Hour).Format(time.RFC3339Nano), nil, now)

	catalogRepo := catalog.NewRepository(db)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	app := New(config.Config{Environment: "test", ListenAddress: addr}, db, deviceService, catalogRepo, customer.NewRepository(db), orders.New(db, catalogRepo), payments.New(db), inventory.New(db), receipts.New(db))
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
			t.Fatalf("live POSService did not become healthy: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	resp, err := client.Get(baseURL + "/api/v1/catalog/products/price-precedence-item")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("catalog lookup status=%d", resp.StatusCode)
	}
	var product catalog.Product
	if err := json.NewDecoder(resp.Body).Decode(&product); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if product.Price == nil || product.Price.AmountMinor != 9500 {
		t.Fatalf("live catalog selected price=%+v want amount_minor=9500", product.Price)
	}

	orderRaw, _ := json.Marshal(map[string]any{
		"client_order_id": "runtime-price-precedence-order-1",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "price-precedence-item", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0,
		}},
	})
	resp, err = client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create order status=%d body=%v", resp.StatusCode, body)
	}
	var order orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if order.SubtotalMinor != 9500 || order.TotalMinor != 9500 {
		t.Fatalf("order totals subtotal=%d total=%d want=9500", order.SubtotalMinor, order.TotalMinor)
	}
	if len(order.Items) != 1 || order.Items[0].UnitPriceMinor != 9500 || order.Items[0].LineTotalMinor != 9500 {
		t.Fatalf("order item price snapshot mismatch: %+v", order.Items)
	}

	var persistedUnitPrice, persistedLineTotal int64
	if err := db.SQL().QueryRow(`SELECT unit_price_minor, line_total_minor FROM sales_order_items WHERE order_id=? AND product_id='price-precedence-item'`, order.ID).Scan(&persistedUnitPrice, &persistedLineTotal); err != nil {
		t.Fatal(err)
	}
	if persistedUnitPrice != 9500 || persistedLineTotal != 9500 {
		t.Fatalf("persisted price snapshot unit=%d line_total=%d want=9500", persistedUnitPrice, persistedLineTotal)
	}
}
