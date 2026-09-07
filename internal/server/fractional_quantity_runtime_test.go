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

func TestFractionalQuantityCashSaleOverLiveHTTPServer(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil {
		t.Fatal(err)
	}

	statements := []string{
		`INSERT INTO catalog_products(id,sku,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES('weighted-rice','RICE-WEIGHT','Weighted Rice','unit',1,0,1,1,'2026-01-01T00:00:00Z')`,
		`INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES('price-weighted-rice','weighted-rice','store-1','INR',20000,1,100,1,'2026-01-01T00:00:00Z')`,
		`INSERT INTO inventory_balances(product_id,store_id,on_hand_milli,reserved_milli,updated_at) VALUES('weighted-rice','store-1',5000,0,'2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed weighted product: %v", err)
		}
	}

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

	orderBody := map[string]any{
		"client_order_id": "runtime-fractional-order-1",
		"currency":        "INR",
		"items": []map[string]any{
			{"product_id": "weighted-rice", "quantity_milli": 1250, "discount_minor": 0, "tax_minor": 0},
		},
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
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	const wantQuantityMilli int64 = 1250
	const wantTotalMinor int64 = 25000 // 1.250 * INR 200.00
	if len(order.Items) != 1 {
		t.Fatalf("order items=%d want=1", len(order.Items))
	}
	if order.Items[0].QuantityMilli != wantQuantityMilli {
		t.Fatalf("quantity_milli=%d want=%d", order.Items[0].QuantityMilli, wantQuantityMilli)
	}
	if order.TotalMinor != wantTotalMinor {
		t.Fatalf("order total=%d want=%d", order.TotalMinor, wantTotalMinor)
	}

	var persistedQuantity, persistedLineTotal int64
	if err := db.SQL().QueryRow(`SELECT quantity_milli, line_total_minor FROM sales_order_items WHERE order_id=? AND product_id='weighted-rice'`, order.ID).Scan(&persistedQuantity, &persistedLineTotal); err != nil {
		t.Fatal(err)
	}
	if persistedQuantity != wantQuantityMilli || persistedLineTotal != wantTotalMinor {
		t.Fatalf("persisted quantity=%d line_total=%d want quantity=%d line_total=%d", persistedQuantity, persistedLineTotal, wantQuantityMilli, wantTotalMinor)
	}

	paymentBody := map[string]any{"client_payment_id": "runtime-fractional-payment-1", "mode": "cash", "amount_minor": order.TotalMinor, "currency": "INR", "status": "captured"}
	paymentRaw, _ := json.Marshal(paymentBody)
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(paymentRaw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("payment status=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/complete", baseURL, order.ID), "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete status=%d", resp.StatusCode)
	}
	var completion struct {
		Order   orders.Order      `json:"order"`
		Receipt receipts.Receipt `json:"receipt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if completion.Order.TotalMinor != wantTotalMinor || completion.Receipt.OrderID != order.ID {
		t.Fatalf("completion mismatch: %#v %#v", completion.Order, completion.Receipt)
	}

	var movementQuantity int64
	if err := db.SQL().QueryRow(`SELECT quantity_delta_milli FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=? AND product_id='weighted-rice'`, order.ID).Scan(&movementQuantity); err != nil {
		t.Fatal(err)
	}
	if movementQuantity != -wantQuantityMilli {
		t.Fatalf("inventory movement quantity=%d want=%d", movementQuantity, -wantQuantityMilli)
	}

	var onHandMilli int64
	if err := db.SQL().QueryRow(`SELECT on_hand_milli FROM inventory_balances WHERE product_id='weighted-rice' AND store_id='store-1'`).Scan(&onHandMilli); err != nil {
		t.Fatal(err)
	}
	if onHandMilli != 3750 {
		t.Fatalf("on_hand_milli=%d want=3750", onHandMilli)
	}
}
