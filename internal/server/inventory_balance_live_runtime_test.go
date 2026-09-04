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
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestCompletedSaleUpdatesInventoryBalanceOverLiveHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil {
		t.Fatal(err)
	}
	seedOrderCatalog(t, db)
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at) VALUES('store-1','product-1',5000,0,1,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	catalogRepo := catalog.NewRepository(db)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
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

	orderRaw, _ := json.Marshal(map[string]any{"client_order_id": "inventory-balance-runtime-1", "currency": "INR", "items": []map[string]any{{"product_id": "product-1", "quantity_milli": 2000, "discount_minor": 0, "tax_minor": 0}}})
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		t.Fatalf("create order status=%d", resp.StatusCode)
	}
	var order orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		_ = resp.Body.Close()
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	paymentRaw, _ := json.Marshal(map[string]any{"client_payment_id": "inventory-balance-payment-1", "mode": "cash", "amount_minor": order.TotalMinor, "currency": "INR", "status": "captured"})
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(paymentRaw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		_ = resp.Body.Close()
		t.Fatalf("payment status=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/complete", baseURL, order.ID), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("complete status=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp, err = client.Get(baseURL + "/api/v1/inventory/balances/product-1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("balance status=%d", resp.StatusCode)
	}
	var balance inventory.Balance
	if err := json.NewDecoder(resp.Body).Decode(&balance); err != nil {
		_ = resp.Body.Close()
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if balance.OnHandMilli != 3000 || balance.AvailableMilli != 3000 || balance.ReservedMilli != 0 {
		t.Fatalf("unexpected balance: %+v", balance)
	}

	resp, err = client.Get(baseURL + "/api/v1/inventory/movements?product_id=product-1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("movements status=%d", resp.StatusCode)
	}
	var movementsPayload struct {
		Items []inventory.Movement `json:"items"`
		Count int                  `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&movementsPayload); err != nil {
		_ = resp.Body.Close()
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if movementsPayload.Count != 1 || len(movementsPayload.Items) != 1 {
		t.Fatalf("unexpected movements: %+v", movementsPayload)
	}
	m := movementsPayload.Items[0]
	if m.MovementType != "sale_issue" || m.QuantityDeltaMilli != -2000 || m.BalanceAfterMilli != 3000 || m.ReferenceID == nil || *m.ReferenceID != order.ID {
		t.Fatalf("unexpected movement: %+v", m)
	}
}