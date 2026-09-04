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

func TestPaymentRetryIsIdempotentOverLiveHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil { t.Fatal(err) }
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil { t.Fatal(err) }

	inboxService := inbox.New(db)
	applyPaymentRuntimeMessage(t, inboxService, "pay-product-1", "catalog.product.upsert", map[string]any{
		"id": "1", "name": "Milk", "unit_of_measure": "unit", "is_active": true,
		"allow_manual_price": false, "track_inventory": true, "version": 1,
	})
	applyPaymentRuntimeMessage(t, inboxService, "pay-price-1", "catalog.price.upsert", map[string]any{
		"id": "price-1", "product_id": "1", "store_id": "store-1", "currency": "INR",
		"amount_minor": 12500, "tax_inclusive": true, "priority": 100, "version": 1,
	})

	catalogRepo := catalog.NewRepository(db)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil { t.Fatal(err) }

	app := New(
		config.Config{Environment: "test", ListenAddress: addr},
		db, deviceService, catalogRepo, customer.NewRepository(db), orders.New(db, catalogRepo),
		payments.New(db), inventory.New(db), receipts.New(db),
	)
	serverErr := make(chan error, 1)
	go func() { serverErr <- app.Start() }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(shutdownCtx)
		select {
		case err := <-serverErr:
			if err != nil { t.Errorf("server shutdown: %v", err) }
		case <-time.After(2 * time.Second):
			t.Error("server did not stop after shutdown")
		}
	})

	baseURL := "http://" + addr
	client := &http.Client{Timeout: 2 * time.Second}
	waitForPaymentRuntimeHealth(t, client, baseURL)

	orderRaw, _ := json.Marshal(map[string]any{
		"client_order_id": "payment-idempotency-order-1",
		"currency": "INR",
		"items": []map[string]any{{"product_id": "1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0}},
	})
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
	if err != nil { t.Fatalf("create order: %v", err) }
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close(); var body any; _ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create order status=%d body=%v", resp.StatusCode, body)
	}
	var order orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil { _ = resp.Body.Close(); t.Fatal(err) }
	_ = resp.Body.Close()

	paymentBody := map[string]any{
		"client_payment_id": "payment-idempotency-1",
		"mode": "cash", "amount_minor": order.TotalMinor, "currency": "INR", "status": "captured",
	}
	first := postPaymentRuntime(t, client, baseURL, order.ID, paymentBody, http.StatusCreated)
	var versionAfterFirst int
	if err := db.SQL().QueryRow(`SELECT version FROM sales_orders WHERE id=?`, order.ID).Scan(&versionAfterFirst); err != nil { t.Fatal(err) }

	second := postPaymentRuntime(t, client, baseURL, order.ID, paymentBody, http.StatusCreated)
	if first.Payment.ID == "" || second.Payment.ID != first.Payment.ID {
		t.Fatalf("idempotent retry returned different payment: first=%q second=%q", first.Payment.ID, second.Payment.ID)
	}
	if second.Summary.PaidMinor != order.TotalMinor || second.Summary.BalanceMinor != 0 {
		t.Fatalf("retry summary paid=%d balance=%d total=%d", second.Summary.PaidMinor, second.Summary.BalanceMinor, order.TotalMinor)
	}

	var versionAfterRetry, paymentCount, recordedEventCount int
	if err := db.SQL().QueryRow(`SELECT version FROM sales_orders WHERE id=?`, order.ID).Scan(&versionAfterRetry); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM payments WHERE client_payment_id=?`, "payment-idempotency-1").Scan(&paymentCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='payment' AND aggregate_id=?`, first.Payment.ID).Scan(&recordedEventCount); err != nil { t.Fatal(err) }
	if versionAfterRetry != versionAfterFirst || paymentCount != 1 || recordedEventCount != 1 {
		t.Fatalf("retry mutated state version_first=%d version_retry=%d payments=%d payment_events=%d", versionAfterFirst, versionAfterRetry, paymentCount, recordedEventCount)
	}

	conflict := map[string]any{
		"client_payment_id": "payment-idempotency-1",
		"mode": "cash", "amount_minor": order.TotalMinor-100, "currency": "INR", "status": "captured",
	}
	conflictRaw, _ := json.Marshal(conflict)
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(conflictRaw))
	if err != nil { t.Fatalf("conflicting retry: %v", err) }
	if resp.StatusCode != http.StatusBadRequest {
		defer resp.Body.Close(); var body any; _ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("conflicting retry status=%d body=%v", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM payments WHERE client_payment_id=?`, "payment-idempotency-1").Scan(&paymentCount); err != nil { t.Fatal(err) }
	if paymentCount != 1 { t.Fatalf("conflicting retry created duplicate payment count=%d", paymentCount) }
}

type paymentRuntimeResponse struct {
	Payment payments.Payment `json:"payment"`
	Summary payments.Summary `json:"summary"`
}

func postPaymentRuntime(t *testing.T, client *http.Client, baseURL, orderID string, body map[string]any, wantStatus int) paymentRuntimeResponse {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, orderID), "application/json", bytes.NewReader(raw))
	if err != nil { t.Fatalf("post payment: %v", err) }
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus { var payload any; _ = json.NewDecoder(resp.Body).Decode(&payload); t.Fatalf("payment status=%d want=%d body=%v", resp.StatusCode, wantStatus, payload) }
	var payload paymentRuntimeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil { t.Fatal(err) }
	return payload
}

func applyPaymentRuntimeMessage(t *testing.T, service *inbox.Service, id, messageType string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload); if err != nil { t.Fatal(err) }
	if err := service.Apply(context.Background(), inbox.Message{ID: id, Type: messageType, SchemaVersion: 1, Source: "central", Payload: raw}); err != nil { t.Fatalf("apply %s: %v", messageType, err) }
}

func waitForPaymentRuntimeHealth(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get(baseURL + "/api/v1/health")
		if err == nil { _ = resp.Body.Close(); if resp.StatusCode == http.StatusOK { return } }
		if time.Now().After(deadline) { t.Fatalf("POSService runtime did not become healthy at %s: %v", baseURL, err) }
		time.Sleep(25 * time.Millisecond)
	}
}
