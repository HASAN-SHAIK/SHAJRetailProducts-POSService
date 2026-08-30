package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func TestV1CustomerReceiptSnapshotRemainsFrozenOverLiveHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	defer db.Close()

	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{
		StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"customer-snapshot-product", "Customer Snapshot Product", "unit", 1, 0, 0, 1, "2026-08-30T08:30:00Z",
	); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"customer-snapshot-price", "customer-snapshot-product", "store-1", "INR", 12500, 1, 100, 1, "2026-08-30T08:30:00Z",
	); err != nil {
		t.Fatalf("insert price: %v", err)
	}

	catalogRepo := catalog.NewRepository(db)
	customerRepo := customer.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: "127.0.0.1:0"},
		db,
		deviceService,
		catalogRepo,
		customerRepo,
		orders.New(db, catalogRepo),
		payments.New(db),
		inventory.New(db),
		receipts.New(db),
	)
	baseURL := startCustomerReceiptRuntime(t, app)

	createdRaw := customerReceiptJSON(t, http.MethodPost, baseURL+"/api/v1/customers", map[string]any{
		"customer_code": "SNAP-001",
		"name":          "Original Customer",
		"phone":         "9000011111",
		"email":         "original.customer@example.test",
		"tax_id":        "GST-SNAP-001",
		"currency":      "INR",
	}, http.StatusCreated)
	var created customer.Customer
	if err := json.Unmarshal(createdRaw, &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Original Customer" {
		t.Fatalf("unexpected created customer: %#v", created)
	}

	orderRaw := customerReceiptJSON(t, http.MethodPost, baseURL+"/api/v1/orders", map[string]any{
		"client_order_id": "customer-snapshot-order-1",
		"customer_id":     created.ID,
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "customer-snapshot-product",
			"quantity_milli": 1000,
			"discount_minor": 0,
			"tax_minor": 0,
		}},
	}, http.StatusCreated)
	var order orders.Order
	if err := json.Unmarshal(orderRaw, &order); err != nil {
		t.Fatal(err)
	}
	if order.CustomerID == nil || *order.CustomerID != created.ID {
		t.Fatalf("live order lost customer identity: %#v", order.CustomerID)
	}
	if order.TotalMinor != 12500 {
		t.Fatalf("order total=%d want=12500", order.TotalMinor)
	}

	customerReceiptJSON(t, http.MethodPost, fmt.Sprintf("%s/api/v1/orders/%s/payments", baseURL, order.ID), map[string]any{
		"client_payment_id": "customer-snapshot-payment-1",
		"mode":              "cash",
		"amount_minor":      order.TotalMinor,
		"currency":          "INR",
		"status":            "captured",
	}, http.StatusCreated)

	completionRaw := customerReceiptJSON(t, http.MethodPost, fmt.Sprintf("%s/api/v1/orders/%s/complete", baseURL, order.ID), nil, http.StatusOK)
	var completion struct {
		Order   orders.Order     `json:"order"`
		Receipt receipts.Receipt `json:"receipt"`
	}
	if err := json.Unmarshal(completionRaw, &completion); err != nil {
		t.Fatal(err)
	}
	assertFrozenCustomerSnapshot(t, completion.Receipt, created.ID, "Original Customer", "original.customer@example.test")

	customerReceiptJSON(t, http.MethodPut, baseURL+"/api/v1/customers/"+created.ID, map[string]any{
		"customer_code": "SNAP-001",
		"name":          "Changed After Sale",
		"phone":         "9000011111",
		"email":         "changed.after.sale@example.test",
		"tax_id":        "GST-SNAP-001",
		"currency":      "INR",
	}, http.StatusOK)

	currentRaw := customerReceiptJSON(t, http.MethodGet, baseURL+"/api/v1/customers/"+created.ID, nil, http.StatusOK)
	var current customer.Customer
	if err := json.Unmarshal(currentRaw, &current); err != nil {
		t.Fatal(err)
	}
	if current.Name != "Changed After Sale" || current.Email == nil || *current.Email != "changed.after.sale@example.test" {
		t.Fatalf("customer update did not persist: %#v", current)
	}

	frozenRaw := customerReceiptJSON(t, http.MethodGet, fmt.Sprintf("%s/api/v1/orders/%s/receipt", baseURL, order.ID), nil, http.StatusOK)
	var frozen receipts.Receipt
	if err := json.Unmarshal(frozenRaw, &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.ID != completion.Receipt.ID {
		t.Fatalf("receipt identity changed after customer edit: got=%q want=%q", frozen.ID, completion.Receipt.ID)
	}
	assertFrozenCustomerSnapshot(t, frozen, created.ID, "Original Customer", "original.customer@example.test")

	var snapshotRaw string
	if err := db.SQL().QueryRowContext(ctx, `SELECT snapshot_json FROM receipts WHERE id=? AND order_id=?`, frozen.ID, order.ID).Scan(&snapshotRaw); err != nil {
		t.Fatalf("read persisted receipt snapshot: %v", err)
	}
	var persisted receipts.Snapshot
	if err := json.Unmarshal([]byte(snapshotRaw), &persisted); err != nil {
		t.Fatalf("decode persisted receipt snapshot: %v", err)
	}
	if persisted.Customer == nil || persisted.Customer.ID != created.ID || persisted.Customer.Name != "Original Customer" || persisted.Customer.Email == nil || *persisted.Customer.Email != "original.customer@example.test" {
		t.Fatalf("SQLite receipt snapshot mutated after customer edit: %#v", persisted.Customer)
	}

	var outboxPayload string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT payload_json FROM outbox_events
		WHERE aggregate_type='sales_order' AND aggregate_id=? AND event_type='sale.completed'
		ORDER BY aggregate_version DESC LIMIT 1`, order.ID).Scan(&outboxPayload); err != nil {
		t.Fatalf("read sale.completed outbox payload: %v", err)
	}
	if !bytes.Contains([]byte(outboxPayload), []byte(`"name":"Original Customer"`)) || bytes.Contains([]byte(outboxPayload), []byte(`"name":"Changed After Sale"`)) {
		t.Fatalf("sale.completed outbox customer snapshot was not frozen: %s", outboxPayload)
	}
}

func assertFrozenCustomerSnapshot(t *testing.T, receipt receipts.Receipt, customerID, name, email string) {
	t.Helper()
	if receipt.CustomerID == nil || *receipt.CustomerID != customerID {
		t.Fatalf("receipt customer id=%#v want=%q", receipt.CustomerID, customerID)
	}
	if receipt.Snapshot.Customer == nil || receipt.Snapshot.Customer.ID != customerID || receipt.Snapshot.Customer.Name != name || receipt.Snapshot.Customer.Email == nil || *receipt.Snapshot.Customer.Email != email {
		t.Fatalf("receipt customer snapshot mismatch: %#v", receipt.Snapshot.Customer)
	}
}

func startCustomerReceiptRuntime(t *testing.T, app *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	app.cfg.ListenAddress = addr
	app.httpServer.Addr = addr
	serverErr := make(chan error, 1)
	go func() { serverErr <- app.Start() }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(shutdownCtx)
		select {
		case err := <-serverErr:
			if err != nil {
				t.Errorf("customer receipt runtime shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("customer receipt runtime did not stop")
		}
	})

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get("http://" + addr + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return "http://" + addr
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("customer receipt POSService runtime did not become healthy: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func customerReceiptJSON(t *testing.T, method, url string, body any, wantStatus int) []byte {
	t.Helper()
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("runtime %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("runtime %s %s status=%d want=%d body=%s", method, url, resp.StatusCode, wantStatus, raw)
	}
	return raw
}
