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

func TestSaleTimeCategorySnapshotSurvivesCatalogRenameOverLiveHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
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
		INSERT INTO catalog_categories(id,name,sort_order,is_active,version,updated_at)
		VALUES(?,?,?,?,?,?)`,
		"cat-beverages", "Beverages", 1, 1, 1, "2026-08-30T10:00:00Z",
	); err != nil {
		t.Fatalf("insert category: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(id,category_id,name,is_active,allow_manual_price,track_inventory,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		"101", "cat-beverages", "Cola", 1, 0, 0, 1, "2026-08-30T10:00:00Z",
	); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"price-101", "101", "store-1", "INR", 10000, 1, 100, 1, "2026-08-30T10:00:00Z",
	); err != nil {
		t.Fatalf("insert price: %v", err)
	}

	catalogRepo := catalog.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: freeCategorySnapshotAddress(t)},
		db,
		deviceService,
		catalogRepo,
		customer.NewRepository(db),
		orders.New(db, catalogRepo),
		payments.New(db),
		inventory.New(db),
		receipts.New(db),
	)

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

	baseURL := "http://" + app.cfg.ListenAddress
	client := &http.Client{Timeout: 2 * time.Second}
	waitForCategorySnapshotHealth(t, client, baseURL)

	payload := map[string]any{
		"client_order_id": "runtime-category-snapshot-order",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "101", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0,
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("create order request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		var body any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("create order status=%d body=%v", resp.StatusCode, body)
	}
	var created orders.Order
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created order: %v", err)
	}
	if len(created.Items) != 1 || created.Items[0].CategoryIDSnapshot == nil || *created.Items[0].CategoryIDSnapshot != "cat-beverages" {
		t.Fatalf("live create lost category id snapshot: %+v", created.Items)
	}
	if created.Items[0].CategoryNameSnapshot == nil || *created.Items[0].CategoryNameSnapshot != "Beverages" {
		t.Fatalf("live create lost category name snapshot: %+v", created.Items[0])
	}

	if _, err := db.SQL().ExecContext(ctx, `UPDATE catalog_categories SET name=?, version=version+1 WHERE id=?`, "Snacks", "cat-beverages"); err != nil {
		t.Fatalf("rename current category: %v", err)
	}

	getResp, err := client.Get(baseURL + "/api/v1/orders/" + created.ID)
	if err != nil {
		t.Fatalf("reload order request: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		var body any
		_ = json.NewDecoder(getResp.Body).Decode(&body)
		t.Fatalf("reload order status=%d body=%v", getResp.StatusCode, body)
	}
	var reloaded orders.Order
	if err := json.NewDecoder(getResp.Body).Decode(&reloaded); err != nil {
		t.Fatalf("decode reloaded order: %v", err)
	}
	if len(reloaded.Items) != 1 || reloaded.Items[0].CategoryIDSnapshot == nil || *reloaded.Items[0].CategoryIDSnapshot != "cat-beverages" {
		t.Fatalf("reloaded order lost category id snapshot: %+v", reloaded.Items)
	}
	if reloaded.Items[0].CategoryNameSnapshot == nil || *reloaded.Items[0].CategoryNameSnapshot != "Beverages" {
		t.Fatalf("catalog rename rewrote historical category snapshot: %+v", reloaded.Items[0])
	}

	var categoryID, categoryName string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT category_id_snapshot, category_name_snapshot
		FROM sales_order_items WHERE order_id=?`, created.ID,
	).Scan(&categoryID, &categoryName); err != nil {
		t.Fatalf("read persisted category snapshot: %v", err)
	}
	if categoryID != "cat-beverages" || categoryName != "Beverages" {
		t.Fatalf("persisted category snapshot mutated: id=%q name=%q", categoryID, categoryName)
	}
}

func freeCategorySnapshotAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitForCategorySnapshotHealth(t *testing.T, client *http.Client, baseURL string) {
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
			t.Fatalf("live POSService did not become healthy at %s: %v", baseURL, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
