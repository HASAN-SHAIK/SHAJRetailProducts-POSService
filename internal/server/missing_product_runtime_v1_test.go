package server

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestV1MissingProductRejectedOverLiveHTTPAndValidOrderStillWorks(t *testing.T) {
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
		"valid-http-product", "Valid Runtime Product", "unit", 1, 0, 0, 1, "2026-08-30T13:00:00Z",
	); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"valid-http-price", "valid-http-product", "store-1", "INR", 7500, 1, 100, 1, "2026-08-30T13:00:00Z",
	); err != nil {
		t.Fatalf("insert price: %v", err)
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
	baseURL := startMissingProductRuntime(t, app)

	rejectedRaw := missingProductJSON(t, http.MethodPost, baseURL+"/api/v1/orders", map[string]any{
		"client_order_id": "missing-product-http-order",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id":     "does-not-exist",
			"quantity_milli": 1000,
			"discount_minor": 0,
			"tax_minor":      0,
		}},
	}, http.StatusConflict)
	var rejected map[string]any
	if err := json.Unmarshal(rejectedRaw, &rejected); err != nil {
		t.Fatal(err)
	}
	if rejected["error"] != "product_not_found" {
		t.Fatalf("missing product error=%v want=product_not_found", rejected["error"])
	}

	var rejectedCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE client_order_id=?`, "missing-product-http-order").Scan(&rejectedCount); err != nil {
		t.Fatalf("count rejected order: %v", err)
	}
	if rejectedCount != 0 {
		t.Fatalf("missing-product order persisted: count=%d", rejectedCount)
	}

	validRaw := missingProductJSON(t, http.MethodPost, baseURL+"/api/v1/orders", map[string]any{
		"client_order_id": "valid-after-missing-product-http-order",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id":     "valid-http-product",
			"quantity_milli": 1000,
			"discount_minor": 0,
			"tax_minor":      0,
		}},
	}, http.StatusCreated)
	var valid orders.Order
	if err := json.Unmarshal(validRaw, &valid); err != nil {
		t.Fatal(err)
	}
	if valid.TotalMinor != 7500 {
		t.Fatalf("valid order total=%d want=7500", valid.TotalMinor)
	}

	var validCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE client_order_id=?`, "valid-after-missing-product-http-order").Scan(&validCount); err != nil {
		t.Fatalf("count valid order: %v", err)
	}
	if validCount != 1 {
		t.Fatalf("valid recovery order persisted count=%d want=1", validCount)
	}
}

func startMissingProductRuntime(t *testing.T, app *Server) string {
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
				t.Errorf("missing product runtime shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("missing product runtime did not stop")
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
			t.Fatalf("missing product POSService runtime did not become healthy: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func missingProductJSON(t *testing.T, method, url string, body any, wantStatus int) []byte {
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
