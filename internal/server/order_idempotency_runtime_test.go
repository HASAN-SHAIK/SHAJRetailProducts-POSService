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

func TestOrderCreateRetryIsIdempotentOverLiveHTTPServer(t *testing.T) {
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

	inboxService := inbox.New(db)
	applyCatalogMessage(t, inboxService, "runtime-idempotency-product", "catalog.product.upsert", map[string]any{
		"id": "1", "name": "Milk", "unit_of_measure": "unit", "is_active": true,
		"allow_manual_price": false, "track_inventory": true, "version": 1,
	})
	applyCatalogMessage(t, inboxService, "runtime-idempotency-price", "catalog.price.upsert", map[string]any{
		"id": "price-1", "product_id": "1", "store_id": "store-1", "currency": "INR",
		"amount_minor": 12500, "tax_inclusive": true, "priority": 100, "version": 1,
	})

	catalogRepo := catalog.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: freeOrderIdempotencyAddress(t)},
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
	waitForOrderIdempotencyHealth(t, client, baseURL)

	payload := map[string]any{
		"client_order_id": "runtime-idempotent-order-1",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0,
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	create := func() orders.Order {
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
		var order orders.Order
		if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
			t.Fatalf("decode order: %v", err)
		}
		return order
	}

	first := create()
	second := create()
	if first.ID == "" || second.ID != first.ID {
		t.Fatalf("retry created a different order: first=%q second=%q", first.ID, second.ID)
	}
	if second.Version != first.Version || second.TotalMinor != first.TotalMinor || second.CreatedAt != first.CreatedAt {
		t.Fatalf("retry mutated order: first=%+v second=%+v", first, second)
	}

	var orderCount, itemCount, snapshotCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM sales_orders WHERE client_order_id=?`, payload["client_order_id"]).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM sales_order_items WHERE order_id=?`, first.ID).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM sales_order_snapshots WHERE order_id=?`, first.ID).Scan(&snapshotCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 || itemCount != 1 || snapshotCount != 1 {
		t.Fatalf("retry duplicated persisted state: orders=%d items=%d snapshots=%d", orderCount, itemCount, snapshotCount)
	}
}

func freeOrderIdempotencyAddress(t *testing.T) string {
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

func waitForOrderIdempotencyHealth(t *testing.T, client *http.Client, baseURL string) {
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
