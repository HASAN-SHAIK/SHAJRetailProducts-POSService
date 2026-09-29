package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestInvalidPaymentsRejectedOverLiveHTTPServer(t *testing.T) {
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
	applyCatalogMessage(t, inboxService, "invalid-payment-product", "catalog.product.upsert", map[string]any{
		"id": "invalid-payment-product-1", "name": "Milk", "unit_of_measure": "unit", "is_active": true,
		"allow_manual_price": false, "track_inventory": true, "version": 1,
	})
	applyCatalogMessage(t, inboxService, "invalid-payment-price", "catalog.price.upsert", map[string]any{
		"id": "invalid-payment-price-1", "product_id": "invalid-payment-product-1", "store_id": "store-1", "currency": "INR",
		"amount_minor": 12500, "tax_inclusive": true, "priority": 100, "version": 1,
	})

	catalogRepo := catalog.NewRepository(db)
	orderService := orders.New(db, catalogRepo)
	paymentService := payments.New(db)
	inventoryService := inventory.New(db)
	receiptService := receipts.New(db)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	app := New(config.Config{Environment: "test", ListenAddress: addr}, db, deviceService, catalogRepo, customer.NewRepository(db), orderService, paymentService, inventoryService, receiptService)
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
			t.Fatalf("live POSService did not become healthy at %s: %v", baseURL, err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	orderBody := map[string]any{
		"client_order_id": "invalid-payment-order-1",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "invalid-payment-product-1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0,
		}},
	}
	orderRaw, _ := json.Marshal(orderBody)
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
	if err != nil {
		t.Fatalf("create order request: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create order status=%d body=%v", resp.StatusCode, body)
	}
	var order orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode order: %v", err)
	}
	_ = resp.Body.Close()
	if order.TotalMinor != 12500 {
		t.Fatalf("order total=%d want=12500", order.TotalMinor)
	}

	invalidCases := []struct {
		name string
		body map[string]any
	}{
		{name: "zero amount", body: map[string]any{"client_payment_id": "bad-zero", "mode": "cash", "amount_minor": 0, "currency": "INR", "status": "captured"}},
		{name: "negative amount", body: map[string]any{"client_payment_id": "bad-negative", "mode": "cash", "amount_minor": -100, "currency": "INR", "status": "captured"}},
		{name: "unsupported mode", body: map[string]any{"client_payment_id": "bad-mode", "mode": "crypto", "amount_minor": 12500, "currency": "INR", "status": "captured"}},
		{name: "wrong currency", body: map[string]any{"client_payment_id": "bad-currency", "mode": "cash", "amount_minor": 12500, "currency": "USD", "status": "captured"}},
		{name: "missing client id", body: map[string]any{"client_payment_id": "", "mode": "cash", "amount_minor": 12500, "currency": "INR", "status": "captured"}},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.body)
			resp, err := client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("payment request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				var body any
				_ = json.NewDecoder(resp.Body).Decode(&body)
				t.Fatalf("status=%d body=%v want=400", resp.StatusCode, body)
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if body["error"] != "invalid_payment" {
				t.Fatalf("error=%v want=invalid_payment", body["error"])
			}
		})
	}

	var invalidPaymentCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM payments WHERE order_id=?`, order.ID).Scan(&invalidPaymentCount); err != nil {
		t.Fatal(err)
	}
	if invalidPaymentCount != 0 {
		t.Fatalf("invalid payment attempts persisted %d payment rows", invalidPaymentCount)
	}

	var status string
	if err := db.SQL().QueryRow(`SELECT status FROM sales_orders WHERE id=?`, order.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != order.Status {
		t.Fatalf("invalid payment attempts mutated order status=%q want=%q", status, order.Status)
	}

	validBody := map[string]any{
		"client_payment_id": "valid-after-invalid", "mode": "cash", "amount_minor": order.TotalMinor, "currency": "INR", "status": "captured",
	}
	validRaw, _ := json.Marshal(validBody)
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(validRaw))
	if err != nil {
		t.Fatalf("valid payment request: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("valid payment status=%d body=%v", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/complete", baseURL, order.ID), "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("complete order request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("complete order status=%d body=%v", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	var paymentCount, receiptCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM payments WHERE order_id=?`, order.ID).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM receipts WHERE order_id=?`, order.ID).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 1 || receiptCount != 1 {
		t.Fatalf("valid recovery flow payment=%d receipt=%d want=1,1", paymentCount, receiptCount)
	}
}
