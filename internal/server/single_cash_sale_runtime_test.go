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

func TestSingleCashSaleOverLiveHTTPServer(t *testing.T) {
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
	applyCatalogMessage(t, inboxService, "runtime-product-1", "catalog.product.upsert", map[string]any{
		"id": "1", "name": "Milk", "unit_of_measure": "unit", "is_active": true,
		"allow_manual_price": false, "track_inventory": true, "version": 1,
	})
	applyCatalogMessage(t, inboxService, "runtime-price-1", "catalog.price.upsert", map[string]any{
		"id": "price-1", "product_id": "1", "store_id": "store-1", "currency": "INR",
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
		"client_order_id": "runtime-order-1",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0,
		}},
	}
	orderRaw, _ := json.Marshal(orderBody)
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
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
	if order.StoreID != "store-1" || order.TerminalID == nil || *order.TerminalID != "POS-01" {
		t.Fatalf("order identity store=%q terminal=%v", order.StoreID, order.TerminalID)
	}
	if order.TotalMinor != 12500 {
		t.Fatalf("order total=%d want=12500", order.TotalMinor)
	}

	paymentBody := map[string]any{
		"client_payment_id": "runtime-payment-1",
		"mode":              "cash",
		"amount_minor":      order.TotalMinor,
		"currency":          "INR",
		"status":            "captured",
	}
	paymentRaw, _ := json.Marshal(paymentBody)
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(paymentRaw))
	if err != nil {
		t.Fatalf("create payment request: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create payment status=%d body=%v", resp.StatusCode, body)
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
	var completion struct {
		Order   orders.Order     `json:"order"`
		Receipt receipts.Receipt `json:"receipt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode completion: %v", err)
	}
	_ = resp.Body.Close()
	if completion.Order.ID != order.ID || completion.Receipt.OrderID != order.ID || completion.Receipt.ID == "" {
		t.Fatalf("invalid completion order=%q receipt=%+v", completion.Order.ID, completion.Receipt)
	}

	resp, err = client.Get(fmt.Sprintf("%s/api/v1/orders/%s/receipt", baseURL, order.ID))
	if err != nil {
		t.Fatalf("get receipt request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("receipt status=%d", resp.StatusCode)
	}
	var persistedReceipt receipts.Receipt
	if err := json.NewDecoder(resp.Body).Decode(&persistedReceipt); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode persisted receipt: %v", err)
	}
	_ = resp.Body.Close()
	if persistedReceipt.ID != completion.Receipt.ID {
		t.Fatalf("receipt id changed: %q != %q", persistedReceipt.ID, completion.Receipt.ID)
	}

	var completedAt string
	if err := db.SQL().QueryRow(`SELECT completed_at FROM sales_orders WHERE id=?`, order.ID).Scan(&completedAt); err != nil {
		t.Fatal(err)
	}
	if completedAt == "" {
		t.Fatal("completed sale was not persisted")
	}
	var paymentCount, receiptCount, movementCount, completionOutboxCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM payments WHERE order_id=? AND mode='cash' AND status='captured'`, order.ID).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM receipts WHERE order_id=?`, order.ID).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=?`, order.ID).Scan(&movementCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='sales_order' AND aggregate_id=? AND event_type='sale.completed'`, order.ID).Scan(&completionOutboxCount); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 1 || receiptCount != 1 || movementCount != 1 || completionOutboxCount != 1 {
		t.Fatalf("payment=%d receipt=%d movement=%d sale.completed outbox=%d", paymentCount, receiptCount, movementCount, completionOutboxCount)
	}
}
