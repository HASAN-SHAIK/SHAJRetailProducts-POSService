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

func TestCompletionRetryDoesNotDuplicateSaleSideEffectsOverLiveHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil { t.Fatal(err) }
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil { t.Fatal(err) }

	inboxService := inbox.New(db)
	applyCatalogMessage(t, inboxService, "completion-retry-product", "catalog.product.upsert", map[string]any{
		"id": "1", "name": "Milk", "unit_of_measure": "unit", "is_active": true,
		"allow_manual_price": false, "track_inventory": true, "version": 1,
	})
	applyCatalogMessage(t, inboxService, "completion-retry-price", "catalog.price.upsert", map[string]any{
		"id": "price-1", "product_id": "1", "store_id": "store-1", "currency": "INR",
		"amount_minor": 12500, "tax_inclusive": true, "priority": 100, "version": 1,
	})

	catalogRepo := catalog.NewRepository(db)
	addr := reserveAddress(t)
	app := New(
		config.Config{Environment: "test", ListenAddress: addr},
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
			if err != nil { t.Errorf("server shutdown: %v", err) }
		case <-time.After(2 * time.Second):
			t.Error("server did not stop after shutdown")
		}
	})

	baseURL := "http://" + addr
	client := &http.Client{Timeout: 2 * time.Second}
	waitForHealthyServer(t, client, baseURL)

	orderRaw, _ := json.Marshal(map[string]any{
		"client_order_id": "completion-retry-order",
		"currency": "INR",
		"items": []map[string]any{{"product_id": "1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0}},
	})
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
	if err != nil { t.Fatalf("create order request: %v", err) }
	if resp.StatusCode != http.StatusCreated { defer resp.Body.Close(); t.Fatalf("create order status=%d", resp.StatusCode) }
	var order orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil { resp.Body.Close(); t.Fatal(err) }
	resp.Body.Close()

	paymentRaw, _ := json.Marshal(map[string]any{
		"client_payment_id": "completion-retry-payment", "mode": "cash",
		"amount_minor": order.TotalMinor, "currency": "INR", "status": "captured",
	})
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(paymentRaw))
	if err != nil { t.Fatalf("create payment request: %v", err) }
	if resp.StatusCode != http.StatusCreated { defer resp.Body.Close(); t.Fatalf("payment status=%d", resp.StatusCode) }
	resp.Body.Close()

	completeURL := fmt.Sprintf("%s/api/v1/orders/%s/complete", baseURL, order.ID)
	resp, err = client.Post(completeURL, "application/json", bytes.NewReader(nil))
	if err != nil { t.Fatalf("first completion request: %v", err) }
	if resp.StatusCode != http.StatusOK { defer resp.Body.Close(); t.Fatalf("first completion status=%d", resp.StatusCode) }
	var first struct { Order orders.Order `json:"order"`; Receipt receipts.Receipt `json:"receipt"` }
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil { resp.Body.Close(); t.Fatal(err) }
	resp.Body.Close()

	resp, err = client.Post(completeURL, "application/json", bytes.NewReader(nil))
	if err != nil { t.Fatalf("retry completion request: %v", err) }
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusBadRequest {
		defer resp.Body.Close(); t.Fatalf("unexpected retry completion status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	var paymentCount, receiptCount, movementCount, completionOutboxCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM payments WHERE order_id=?`, order.ID).Scan(&paymentCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM receipts WHERE order_id=?`, order.ID).Scan(&receiptCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=?`, order.ID).Scan(&movementCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='sales_order' AND aggregate_id=? AND event_type='sale.completed'`, order.ID).Scan(&completionOutboxCount); err != nil { t.Fatal(err) }
	if paymentCount != 1 || receiptCount != 1 || movementCount != 1 || completionOutboxCount != 1 {
		t.Fatalf("completion retry duplicated side effects: payments=%d receipts=%d movements=%d sale.completed=%d", paymentCount, receiptCount, movementCount, completionOutboxCount)
	}

	resp, err = client.Get(fmt.Sprintf("%s/api/v1/orders/%s/receipt", baseURL, order.ID))
	if err != nil { t.Fatalf("get receipt request: %v", err) }
	if resp.StatusCode != http.StatusOK { defer resp.Body.Close(); t.Fatalf("receipt status=%d", resp.StatusCode) }
	var persisted receipts.Receipt
	if err := json.NewDecoder(resp.Body).Decode(&persisted); err != nil { resp.Body.Close(); t.Fatal(err) }
	resp.Body.Close()
	if persisted.ID != first.Receipt.ID || persisted.OrderID != order.ID {
		t.Fatalf("receipt changed after completion retry: first=%+v persisted=%+v", first.Receipt, persisted)
	}
}

func reserveAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil { t.Fatal(err) }
	return addr
}

func waitForHealthyServer(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get(baseURL + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK { return }
		}
		if time.Now().After(deadline) { t.Fatalf("live POSService did not become healthy at %s: %v", baseURL, err) }
		time.Sleep(25 * time.Millisecond)
	}
}
