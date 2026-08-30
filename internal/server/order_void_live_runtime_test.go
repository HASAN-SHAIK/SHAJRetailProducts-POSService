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

func TestDraftOrderVoidOverLiveHTTPIsDurableAndRetrySafe(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil { t.Fatal(err) }
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil { t.Fatal(err) }
	seedOrderCatalog(t, db)

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
			if resp.StatusCode == http.StatusOK { break }
		}
		if time.Now().After(deadline) { t.Fatalf("POSService did not become healthy: %v", err) }
		time.Sleep(25 * time.Millisecond)
	}

	orderRaw, _ := json.Marshal(map[string]any{"client_order_id": "void-live-runtime-1", "currency": "INR", "items": []map[string]any{{"product_id": "product-1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0}}})
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != http.StatusCreated { _ = resp.Body.Close(); t.Fatalf("create order status=%d", resp.StatusCode) }
	var order orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil { _ = resp.Body.Close(); t.Fatal(err) }
	_ = resp.Body.Close()

	voidRaw, _ := json.Marshal(map[string]any{"reason": "customer changed mind"})
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/void", baseURL, order.ID), "application/json", bytes.NewReader(voidRaw))
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != http.StatusOK { _ = resp.Body.Close(); t.Fatalf("void status=%d", resp.StatusCode) }
	_ = resp.Body.Close()

	resp, err = client.Get(fmt.Sprintf("%s/api/v1/orders/%s", baseURL, order.ID))
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != http.StatusOK { _ = resp.Body.Close(); t.Fatalf("get order status=%d", resp.StatusCode) }
	var persisted orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&persisted); err != nil { _ = resp.Body.Close(); t.Fatal(err) }
	_ = resp.Body.Close()
	if persisted.Status != "cancelled" || persisted.CompletedAt != nil || persisted.Version != order.Version+1 {
		t.Fatalf("unexpected persisted voided order: %+v", persisted)
	}

	var voidedBy, approvedBy, reason string
	if err := db.SQL().QueryRowContext(ctx, `SELECT COALESCE(voided_by_user_id,''), COALESCE(approved_by_user_id,''), COALESCE(approval_reason,'') FROM sales_orders WHERE id=?`, order.ID).Scan(&voidedBy, &approvedBy, &reason); err != nil { t.Fatal(err) }
	if voidedBy != "internal-test" || approvedBy != "internal-test" || reason != "customer changed mind" {
		t.Fatalf("unexpected void audit: voided_by=%q approved_by=%q reason=%q", voidedBy, approvedBy, reason)
	}

	var receipts, movements int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM receipts WHERE order_id=?`, order.ID).Scan(&receipts); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE reference_id=?`, order.ID).Scan(&movements); err != nil { t.Fatal(err) }
	if receipts != 0 || movements != 0 { t.Fatalf("voided draft created side effects: receipts=%d movements=%d", receipts, movements) }

	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/void", baseURL, order.ID), "application/json", bytes.NewReader(voidRaw))
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != http.StatusConflict { _ = resp.Body.Close(); t.Fatalf("second void status=%d", resp.StatusCode) }
	_ = resp.Body.Close()

	var version int
	if err := db.SQL().QueryRowContext(ctx, `SELECT version FROM sales_orders WHERE id=?`, order.ID).Scan(&version); err != nil { t.Fatal(err) }
	if version != persisted.Version { t.Fatalf("retry mutated voided order version: got=%d want=%d", version, persisted.Version) }
}
