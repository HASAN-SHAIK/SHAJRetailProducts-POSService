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

func TestMultiItemCashSaleOverLiveHTTPServer(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil { t.Fatal(err) }
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil { t.Fatal(err) }

	statements := []string{
		`INSERT INTO catalog_products(id,sku,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES('milk','MILK-500','Milk 500ml','unit',1,0,1,1,'2026-01-01T00:00:00Z')`,
		`INSERT INTO catalog_products(id,sku,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES('rice','RICE-1KG','Rice 1kg','unit',1,0,1,1,'2026-01-01T00:00:00Z')`,
		`INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES('price-milk','milk','store-1','INR',3200,1,100,1,'2026-01-01T00:00:00Z')`,
		`INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES('price-rice','rice','store-1','INR',8500,1,100,1,'2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil { t.Fatalf("seed catalog: %v", err) }
	}

	catalogRepo := catalog.NewRepository(db)
	app := New(config.Config{Environment: "test"}, db, deviceService, catalogRepo, customer.NewRepository(db), orders.New(db, catalogRepo), payments.New(db), inventory.New(db), receipts.New(db))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	addr := ln.Addr().String()
	_ = ln.Close()
	app = New(config.Config{Environment: "test", ListenAddress: addr}, db, deviceService, catalogRepo, customer.NewRepository(db), orders.New(db, catalogRepo), payments.New(db), inventory.New(db), receipts.New(db))
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
		if time.Now().After(deadline) { t.Fatalf("live POSService did not become healthy: %v", err) }
		time.Sleep(25 * time.Millisecond)
	}

	orderBody := map[string]any{
		"client_order_id": "runtime-multi-order-1",
		"currency": "INR",
		"items": []map[string]any{
			{"product_id": "milk", "quantity_milli": 2000, "discount_minor": 0, "tax_minor": 0},
			{"product_id": "rice", "quantity_milli": 3000, "discount_minor": 0, "tax_minor": 0},
		},
	}
	orderRaw, _ := json.Marshal(orderBody)
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
	if err != nil { t.Fatalf("create order request: %v", err) }
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		var body any; _ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create order status=%d body=%v", resp.StatusCode, body)
	}
	var order orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil { t.Fatal(err) }
	_ = resp.Body.Close()
	const wantTotal int64 = 319000 // 2*32.00 + 3*85.00
	if order.TotalMinor != wantTotal { t.Fatalf("order total=%d want=%d", order.TotalMinor, wantTotal) }
	if len(order.Items) != 2 { t.Fatalf("order items=%d want=2", len(order.Items)) }

	paymentBody := map[string]any{"client_payment_id": "runtime-multi-payment-1", "mode": "cash", "amount_minor": order.TotalMinor, "currency": "INR", "status": "captured"}
	paymentRaw, _ := json.Marshal(paymentBody)
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(paymentRaw))
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != http.StatusCreated { t.Fatalf("payment status=%d", resp.StatusCode) }
	_ = resp.Body.Close()

	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/complete", baseURL, order.ID), "application/json", bytes.NewReader(nil))
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != http.StatusOK { t.Fatalf("complete status=%d", resp.StatusCode) }
	var completion struct { Order orders.Order `json:"order"`; Receipt receipts.Receipt `json:"receipt"` }
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil { t.Fatal(err) }
	_ = resp.Body.Close()
	if completion.Order.TotalMinor != wantTotal || completion.Receipt.OrderID != order.ID { t.Fatalf("completion mismatch: %#v %#v", completion.Order, completion.Receipt) }

	var paymentCount, receiptCount, movementCount, completionOutboxCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM payments WHERE order_id=? AND mode='cash' AND status='captured'`, order.ID).Scan(&paymentCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM receipts WHERE order_id=?`, order.ID).Scan(&receiptCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=?`, order.ID).Scan(&movementCount); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='sales_order' AND aggregate_id=? AND event_type='sale.completed'`, order.ID).Scan(&completionOutboxCount); err != nil { t.Fatal(err) }
	if paymentCount != 1 || receiptCount != 1 || movementCount != 2 || completionOutboxCount != 1 {
		t.Fatalf("payment=%d receipt=%d movements=%d sale.completed outbox=%d", paymentCount, receiptCount, movementCount, completionOutboxCount)
	}

	rows, err := db.SQL().Query(`SELECT product_id, quantity_milli FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=? ORDER BY product_id`, order.ID)
	if err != nil { t.Fatal(err) }
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() { var productID string; var quantity int64; if err := rows.Scan(&productID, &quantity); err != nil { t.Fatal(err) }; got[productID] = quantity }
	if err := rows.Err(); err != nil { t.Fatal(err) }
	if got["milk"] != -2000 || got["rice"] != -3000 { t.Fatalf("inventory quantities=%v want milk=-2000 rice=-3000", got) }
}
