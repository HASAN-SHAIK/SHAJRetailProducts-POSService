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

func TestV1InactiveCustomerRejectedOverLiveHTTPWithoutBlockingAnonymousSale(t *testing.T) {
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
		"inactive-http-product", "Inactive Customer Boundary Product", "unit", 1, 0, 0, 1, "2026-08-30T10:00:00Z",
	); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"inactive-http-price", "inactive-http-product", "store-1", "INR", 10000, 1, 100, 1, "2026-08-30T10:00:00Z",
	); err != nil {
		t.Fatalf("insert price: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO customers(
			id,name,credit_limit_minor,outstanding_minor,currency,status,created_at,updated_at,local_version,sync_state
		) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"inactive-http-customer", "Inactive Runtime Customer", 0, 0, "INR", "inactive",
		"2026-08-30T10:00:00Z", "2026-08-30T10:00:00Z", 1, "synced",
	); err != nil {
		t.Fatalf("insert inactive customer: %v", err)
	}

	catalogRepo := catalog.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: "127.0.0.1:0"},
		db,
		deviceService,
		catalogRepo,
		customer.NewRepository(db),
		orders.New(db, catalogRepo),
		payments.New(db),
		inventory.New(db),
		receipts.New(db),
	)
	baseURL := startInactiveCustomerRuntime(t, app)

	rejectedRaw := inactiveCustomerJSON(t, http.MethodPost, baseURL+"/api/v1/orders", map[string]any{
		"client_order_id": "inactive-http-order",
		"customer_id":     "inactive-http-customer",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id":     "inactive-http-product",
			"quantity_milli": 1000,
			"discount_minor": 0,
			"tax_minor":      0,
		}},
	}, http.StatusBadRequest)
	var rejected map[string]any
	if err := json.Unmarshal(rejectedRaw, &rejected); err != nil {
		t.Fatal(err)
	}
	if rejected["error"] != "order_create_failed" {
		t.Fatalf("inactive customer error=%v want=order_create_failed", rejected["error"])
	}

	var rejectedCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE client_order_id=?`, "inactive-http-order").Scan(&rejectedCount); err != nil {
		t.Fatalf("count rejected order: %v", err)
	}
	if rejectedCount != 0 {
		t.Fatalf("inactive customer order persisted: count=%d", rejectedCount)
	}

	anonymousRaw := inactiveCustomerJSON(t, http.MethodPost, baseURL+"/api/v1/orders", map[string]any{
		"client_order_id": "anonymous-after-inactive-http-order",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id":     "inactive-http-product",
			"quantity_milli": 1000,
			"discount_minor": 0,
			"tax_minor":      0,
		}},
	}, http.StatusCreated)
	var anonymous orders.Order
	if err := json.Unmarshal(anonymousRaw, &anonymous); err != nil {
		t.Fatal(err)
	}
	if anonymous.CustomerID != nil {
		t.Fatalf("anonymous order customer id=%#v want=nil", anonymous.CustomerID)
	}
	if anonymous.TotalMinor != 10000 {
		t.Fatalf("anonymous order total=%d want=10000", anonymous.TotalMinor)
	}

	var persistedAnonymous int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE client_order_id=? AND customer_id IS NULL`, "anonymous-after-inactive-http-order").Scan(&persistedAnonymous); err != nil {
		t.Fatalf("count anonymous order: %v", err)
	}
	if persistedAnonymous != 1 {
		t.Fatalf("anonymous recovery order persisted count=%d want=1", persistedAnonymous)
	}
}

func startInactiveCustomerRuntime(t *testing.T, app *Server) string {
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
				t.Errorf("inactive customer runtime shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("inactive customer runtime did not stop")
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
			t.Fatalf("inactive customer POSService runtime did not become healthy: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func inactiveCustomerJSON(t *testing.T, method, url string, body any, wantStatus int) []byte {
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

var _ = fmt.Sprintf
