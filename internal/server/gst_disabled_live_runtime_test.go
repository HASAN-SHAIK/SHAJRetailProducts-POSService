package server

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestGSTDisabledOverridesCallerTaxOverLiveHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil { t.Fatal(err) }
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil { t.Fatal(err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,tax_code,gst_rate_bps,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "gst-disabled-product", "GST Disabled Item", "HSN-18", 1800, 1, 0, 0, 1, "2026-08-31T00:00:00Z"); err != nil { t.Fatal(err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "gst-disabled-price", "gst-disabled-product", "store-1", "INR", 10000, 1, 100, 1, "2026-08-31T00:00:00Z"); err != nil { t.Fatal(err) }

	catalogRepo := catalog.NewRepository(db)
	orderService := orders.New(db, catalogRepo)
	orderService.SetTaxPolicy(func(context.Context) (orders.TaxPolicy, error) {
		return orders.TaxPolicy{Enabled: false, Mode: "INCLUSIVE", RoundingMode: "HALF_UP"}, nil
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil { t.Fatal(err) }
	app := New(config.Config{Environment: "test", ListenAddress: addr}, db, deviceService, catalogRepo, customer.NewRepository(db), orderService, payments.New(db), inventory.New(db), receipts.New(db))
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

	raw, err := json.Marshal(map[string]any{
		"client_order_id": "gst-disabled-live",
		"currency": "INR",
		"items": []map[string]any{{"product_id": "gst-disabled-product", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 9999}},
	})
	if err != nil { t.Fatal(err) }
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(raw))
	if err != nil { t.Fatal(err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated { t.Fatalf("create status=%d want %d", resp.StatusCode, http.StatusCreated) }
	var created orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil { t.Fatal(err) }
	if created.TaxMinor != 0 || created.TotalMinor != 10000 { t.Fatalf("GST-disabled totals tax=%d total=%d", created.TaxMinor, created.TotalMinor) }
	if len(created.Items) != 1 || created.Items[0].TaxMinor != 0 { t.Fatalf("GST-disabled line snapshot mismatch: %+v", created.Items) }

	var persistedTax, persistedTotal int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT tax_minor,total_minor FROM sales_orders WHERE id=?`, created.ID).Scan(&persistedTax, &persistedTotal); err != nil { t.Fatal(err) }
	if persistedTax != 0 || persistedTotal != 10000 { t.Fatalf("persisted order tax=%d total=%d", persistedTax, persistedTotal) }
	var lineTax int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT tax_minor FROM sales_order_items WHERE order_id=?`, created.ID).Scan(&lineTax); err != nil { t.Fatal(err) }
	if lineTax != 0 { t.Fatalf("persisted line tax=%d want 0", lineTax) }
}
