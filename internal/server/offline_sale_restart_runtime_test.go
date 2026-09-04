package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
)

func TestOfflineCompletedSaleSurvivesLivePOSServiceRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "offline-restart.db")

	db := openRestartRuntimeDB(t, ctx, dbPath)
	deviceService := device.New(db)
	identity, err := deviceService.EnsureInstallation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{
		StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01",
	}); err != nil {
		t.Fatal(err)
	}

	inboxService := inbox.New(db)
	applyOfflineRestartCatalogMessage(t, inboxService, "offline-product-1", "catalog.product.upsert", map[string]any{
		"id": "1", "name": "Milk", "unit_of_measure": "unit", "is_active": true,
		"allow_manual_price": false, "track_inventory": true, "version": 1,
	})
	applyOfflineRestartCatalogMessage(t, inboxService, "offline-price-1", "catalog.price.upsert", map[string]any{
		"id": "price-1", "product_id": "1", "store_id": "store-1", "currency": "INR",
		"amount_minor": 12500, "tax_inclusive": true, "priority": 100, "version": 1,
	})

	app, baseURL := startOfflineRestartRuntime(t, db)
	client := &http.Client{Timeout: 2 * time.Second}
	waitForRestartRuntimeHealth(t, client, baseURL)

	orderBody := map[string]any{
		"client_order_id": "offline-restart-order-1",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0,
		}},
	}
	orderRaw, _ := json.Marshal(orderBody)
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(orderRaw))
	if err != nil {
		t.Fatalf("create offline order request: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create offline order status=%d body=%v", resp.StatusCode, body)
	}
	var order orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode offline order: %v", err)
	}
	_ = resp.Body.Close()
	if order.TotalMinor != 12500 {
		t.Fatalf("offline order total=%d want=12500", order.TotalMinor)
	}

	paymentBody := map[string]any{
		"client_payment_id": "offline-restart-payment-1",
		"mode":              "cash",
		"amount_minor":      order.TotalMinor,
		"currency":          "INR",
		"status":            "captured",
	}
	paymentRaw, _ := json.Marshal(paymentBody)
	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), "application/json", bytes.NewReader(paymentRaw))
	if err != nil {
		t.Fatalf("create offline payment request: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create offline payment status=%d body=%v", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	resp, err = client.Post(fmt.Sprintf("%s/api/v1/orders/%s/complete", baseURL, order.ID), "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("complete offline order request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("complete offline order status=%d body=%v", resp.StatusCode, body)
	}
	var completion struct {
		Order   orders.Order     `json:"order"`
		Receipt receipts.Receipt `json:"receipt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode offline completion: %v", err)
	}
	_ = resp.Body.Close()
	// A fully captured order is already in the paid business state before the
	// completion endpoint stamps completed_at and emits the immutable receipt.
	if completion.Order.Status != "paid" || completion.Order.CompletedAt == nil || completion.Receipt.ID == "" {
		t.Fatalf("offline completion status=%q completed_at=%v receipt=%q", completion.Order.Status, completion.Order.CompletedAt, completion.Receipt.ID)
	}

	var pendingBeforeRestart int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id=? AND status='pending'`, order.ID).Scan(&pendingBeforeRestart); err != nil {
		t.Fatal(err)
	}
	if pendingBeforeRestart == 0 {
		t.Fatal("completed offline sale did not leave durable pending outbox work")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := app.Shutdown(shutdownCtx); err != nil {
		cancel()
		t.Fatalf("shutdown first POSService runtime: %v", err)
	}
	cancel()
	if err := db.Close(); err != nil {
		t.Fatalf("close SQLite before restart: %v", err)
	}

	db = openRestartRuntimeDB(t, ctx, dbPath)
	restartedApp, restartedURL := startOfflineRestartRuntime(t, db)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = restartedApp.Shutdown(shutdownCtx)
		_ = db.Close()
	}()
	waitForRestartRuntimeHealth(t, client, restartedURL)

	resp, err = client.Get(restartedURL + "/api/v1/device")
	if err != nil {
		t.Fatalf("read device after restart: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("device after restart status=%d", resp.StatusCode)
	}
	var restartedIdentity device.Identity
	if err := json.NewDecoder(resp.Body).Decode(&restartedIdentity); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode restarted device: %v", err)
	}
	_ = resp.Body.Close()
	if restartedIdentity.DeviceID != identity.DeviceID ||
		restartedIdentity.StoreNumber == nil || *restartedIdentity.StoreNumber != "STORE-001" ||
		restartedIdentity.POSNo == nil || *restartedIdentity.POSNo != "POS-01" ||
		restartedIdentity.TouchpointID == nil || *restartedIdentity.TouchpointID != "TP-01" {
		t.Fatalf("device identity changed across restart: before=%+v after=%+v", identity, restartedIdentity)
	}

	resp, err = client.Get(fmt.Sprintf("%s/api/v1/orders/%s", restartedURL, order.ID))
	if err != nil {
		t.Fatalf("read order after restart: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("order after restart status=%d", resp.StatusCode)
	}
	var restartedOrder orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&restartedOrder); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode restarted order: %v", err)
	}
	_ = resp.Body.Close()
	if restartedOrder.ID != order.ID || restartedOrder.Status != "paid" || restartedOrder.TotalMinor != 12500 || restartedOrder.CompletedAt == nil {
		t.Fatalf("completed order changed across restart: %+v", restartedOrder)
	}

	resp, err = client.Get(fmt.Sprintf("%s/api/v1/orders/%s/receipt", restartedURL, order.ID))
	if err != nil {
		t.Fatalf("read receipt after restart: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("receipt after restart status=%d", resp.StatusCode)
	}
	var restartedReceipt receipts.Receipt
	if err := json.NewDecoder(resp.Body).Decode(&restartedReceipt); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode restarted receipt: %v", err)
	}
	_ = resp.Body.Close()
	if restartedReceipt.ID != completion.Receipt.ID || restartedReceipt.OrderID != order.ID {
		t.Fatalf("receipt changed across restart: before=%+v after=%+v", completion.Receipt, restartedReceipt)
	}

	var paymentCount, receiptCount, movementCount, pendingAfterRestart int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM payments WHERE order_id=? AND mode='cash' AND status='captured'`, order.ID).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM receipts WHERE order_id=?`, order.ID).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=?`, order.ID).Scan(&movementCount); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id=? AND status='pending'`, order.ID).Scan(&pendingAfterRestart); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 1 || receiptCount != 1 || movementCount != 1 || pendingAfterRestart != pendingBeforeRestart {
		t.Fatalf("restart persistence payment=%d receipt=%d movement=%d pending_before=%d pending_after=%d", paymentCount, receiptCount, movementCount, pendingBeforeRestart, pendingAfterRestart)
	}
}

func openRestartRuntimeDB(t *testing.T, ctx context.Context, path string) *database.DB {
	t.Helper()
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatalf("open runtime database: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("migrate runtime database: %v", err)
	}
	return db
}

func applyOfflineRestartCatalogMessage(t *testing.T, service *inbox.Service, id, messageType string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(context.Background(), inbox.Message{ID: id, Type: messageType, SchemaVersion: 1, Source: "central", Payload: raw}); err != nil {
		t.Fatalf("apply %s: %v", messageType, err)
	}
}

func startOfflineRestartRuntime(t *testing.T, db *database.DB) (*Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	deviceService := device.New(db)
	catalogRepo := catalog.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: addr, CentralAPIURL: "http://127.0.0.1:1"},
		db,
		deviceService,
		catalogRepo,
		customer.NewRepository(db),
		orders.New(db, catalogRepo),
		payments.New(db),
		inventory.New(db),
		receipts.New(db),
	)
	go func() { _ = app.Start() }()
	return app, "http://" + addr
}

func waitForRestartRuntimeHealth(t *testing.T, client *http.Client, baseURL string) {
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
			t.Fatalf("POSService runtime did not become healthy at %s: %v", baseURL, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
