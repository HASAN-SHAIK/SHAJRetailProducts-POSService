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

func TestCashOverpaymentCompletesAndClampsReceiptBalanceOverLiveHTTPServer(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil { t.Fatal(err) }
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil { t.Fatal(err) }

	inboxService := inbox.New(db)
	applyCatalogMessage(t, inboxService, "overpay-product-1", "catalog.product.upsert", map[string]any{
		"id": "1", "name": "Milk", "unit_of_measure": "unit", "is_active": true,
		"allow_manual_price": false, "track_inventory": true, "version": 1,
	})
	applyCatalogMessage(t, inboxService, "overpay-price-1", "catalog.price.upsert", map[string]any{
		"id": "price-1", "product_id": "1", "store_id": "store-1", "currency": "INR",
		"amount_minor": 12500, "tax_inclusive": true, "priority": 100, "version": 1,
	})

	catalogRepo := catalog.NewRepository(db)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil { t.Fatal(err) }

	app := New(config.Config{Environment: "test", ListenAddress: addr}, db, deviceService, catalogRepo, customer.NewRepository(db), orders.New(db, catalogRepo), payments.New(db), inventory.New(db), receipts.New(db))
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
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get(baseURL + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK { break }
		}
		if time.Now().After(deadline) { t.Fatalf("live POSService did not become healthy at %s: %v", baseURL, err) }
		time.Sleep(25 * time.Millisecond)
	}

	orderRaw, _ := json.Marshal(map[string]any{
		"client_order_id": "overpay-order-1",
		"currency": "INR",
		"items": []map[string]any{{"product_id": "1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0}},
	})
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
	if err != nil { t.Fatalf("create order request: %v", err) }
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close(); var body any; _ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create order status=%d body=%v", resp.StatusCode, body)
	}
	var order orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil { _ = resp.Body.Close(); t.Fatalf("decode order: %v", err) }
	_ = resp.Body.Close()
	if order.TotalMinor != 12500 { t.Fatalf("order total=%d want=12500", order.TotalMinor) }

	paymentRaw, _ := json.Marshal(map[string]any{
		"client_payment_id": "overpay-payment-1",
		"mode": "cash",
		"amount_minor": 13000,
		"currency": "INR",
		"status": "captured",
	})
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(paymentRaw))
	if err != nil { t.Fatalf("create overpayment request: %v", err) }
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close(); var body any; _ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("overpayment status=%d body=%v", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/complete", baseURL, order.ID), "application/json", bytes.NewReader(nil))
	if err != nil { t.Fatalf("complete overpaid order request: %v", err) }
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close(); var body any; _ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("overpaid completion status=%d body=%v", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	var paidMinor, balanceMinor int64
	if err := db.SQL().QueryRow(`SELECT paid_minor,balance_minor FROM receipts WHERE order_id=?`, order.ID).Scan(&paidMinor, &balanceMinor); err != nil { t.Fatal(err) }
	if paidMinor != 13000 || balanceMinor != 0 { t.Fatalf("receipt paid=%d balance=%d want paid=13000 balance=0", paidMinor, balanceMinor) }

	var paymentCount, receiptCount, movementCount, completionOutboxCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM payments WHERE order_id=? AND status='captured'`, order.ID).Scan(&paymentCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM receipts WHERE order_id=?`, order.ID).Scan(&receiptCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=?`, order.ID).Scan(&movementCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='sales_order' AND aggregate_id=? AND event_type='sale.completed'`, order.ID).Scan(&completionOutboxCount); err != nil { t.Fatal(err) }
	if paymentCount != 1 || receiptCount != 1 || movementCount != 1 || completionOutboxCount != 1 {
		t.Fatalf("overpayment completion side effects: payment=%d receipt=%d movement=%d sale.completed=%d", paymentCount, receiptCount, movementCount, completionOutboxCount)
	}
}
