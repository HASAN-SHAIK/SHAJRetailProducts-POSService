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

func TestGSTPolicySnapshotsTaxOverLiveHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(id,name,tax_code,gst_rate_bps,is_active,allow_manual_price,track_inventory,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, "gst-product-1", "GST Item", "HSN-GST-18", 1800, 1, 0, 0, 1, "2026-08-30T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, "gst-price-1", "gst-product-1", "store-1", "INR", 10000, 1, 100, 1, "2026-08-30T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	catalogRepo := catalog.NewRepository(db)
	orderService := orders.New(db, catalogRepo)
	orderService.SetTaxPolicy(func(context.Context) (orders.TaxPolicy, error) {
		return orders.TaxPolicy{Enabled: true, Mode: "EXCLUSIVE", RoundingMode: "HALF_UP"}, nil
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	app := New(config.Config{Environment: "test", ListenAddress: addr}, db, deviceService, catalogRepo, customer.NewRepository(db), orderService, payments.New(db), inventory.New(db), receipts.New(db))
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

	createOrder := func(clientOrderID string, callerTaxMinor int64) orders.Order {
		raw, err := json.Marshal(map[string]any{
			"client_order_id": clientOrderID,
			"currency":        "INR",
			"items": []map[string]any{{
				"product_id": "gst-product-1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": callerTaxMinor,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s status=%d want %d", clientOrderID, resp.StatusCode, http.StatusCreated)
		}
		var created orders.Order
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			t.Fatal(err)
		}
		return created
	}

	exclusive := createOrder("gst-exclusive-live", 9999)
	if exclusive.TaxMinor != 1800 || exclusive.TotalMinor != 11800 {
		t.Fatalf("exclusive GST mismatch: tax=%d total=%d", exclusive.TaxMinor, exclusive.TotalMinor)
	}
	if len(exclusive.Items) != 1 || exclusive.Items[0].TaxMinor != 1800 || exclusive.Items[0].TaxCode == nil || *exclusive.Items[0].TaxCode != "HSN-GST-18" {
		t.Fatalf("exclusive GST line snapshot mismatch: %+v", exclusive.Items)
	}
	if exclusive.Items[0].TaxableMinor == nil || *exclusive.Items[0].TaxableMinor != 10000 || exclusive.Items[0].GSTRateBps == nil || *exclusive.Items[0].GSTRateBps != 1800 {
		t.Fatalf("exclusive GST taxable/rate snapshot mismatch: %+v", exclusive.Items[0])
	}

	orderService.SetTaxPolicy(func(context.Context) (orders.TaxPolicy, error) {
		return orders.TaxPolicy{Enabled: true, Mode: "INCLUSIVE", RoundingMode: "HALF_UP"}, nil
	})
	inclusive := createOrder("gst-inclusive-live", 7777)
	if inclusive.TaxMinor != 1525 || inclusive.TotalMinor != 10000 {
		t.Fatalf("inclusive GST mismatch: tax=%d total=%d", inclusive.TaxMinor, inclusive.TotalMinor)
	}
	if len(inclusive.Items) != 1 || inclusive.Items[0].TaxMinor != 1525 || inclusive.Items[0].TaxCode == nil || *inclusive.Items[0].TaxCode != "HSN-GST-18" {
		t.Fatalf("inclusive GST line snapshot mismatch: %+v", inclusive.Items)
	}

	var orderCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_orders WHERE client_order_id IN (?,?)`, "gst-exclusive-live", "gst-inclusive-live").Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 2 {
		t.Fatalf("persisted GST orders=%d want 2", orderCount)
	}

	var exclusiveTax, exclusiveTaxable, exclusiveRate int64
	var exclusiveCode string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT tax_minor, taxable_minor, gst_rate_bps, tax_code
		FROM sales_order_items WHERE order_id=?`, exclusive.ID).Scan(&exclusiveTax, &exclusiveTaxable, &exclusiveRate, &exclusiveCode); err != nil {
		t.Fatal(err)
	}
	if exclusiveTax != 1800 || exclusiveTaxable != 10000 || exclusiveRate != 1800 || exclusiveCode != "HSN-GST-18" {
		t.Fatalf("persisted exclusive GST snapshot tax=%d taxable=%d rate=%d code=%q", exclusiveTax, exclusiveTaxable, exclusiveRate, exclusiveCode)
	}

	var inclusiveTax, inclusiveRate int64
	var inclusiveCode string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT tax_minor, gst_rate_bps, tax_code
		FROM sales_order_items WHERE order_id=?`, inclusive.ID).Scan(&inclusiveTax, &inclusiveRate, &inclusiveCode); err != nil {
		t.Fatal(err)
	}
	if inclusiveTax != 1525 || inclusiveRate != 1800 || inclusiveCode != "HSN-GST-18" {
		t.Fatalf("persisted inclusive GST snapshot tax=%d rate=%d code=%q", inclusiveTax, inclusiveRate, inclusiveCode)
	}
}
