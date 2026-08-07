package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/observability"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
)

func TestOrderVerticalSlicePersistsAcrossSQLiteRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")

	db := openMigratedDB(t, dbPath)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", TerminalID: "terminal-1"}); err != nil {
		t.Fatal(err)
	}
	seedOrderCatalog(t, db)

	app := newTestServer(db, deviceService)

	orderID := postOrder(t, app)
	postPayment(t, app, orderID, "client-payment-1")
	completeOrder(t, app, orderID)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openMigratedDB(t, dbPath)
	defer reopened.Close()

	assertCount(t, reopened, `SELECT COUNT(*) FROM sales_orders WHERE id=? AND completed_at IS NOT NULL`, orderID, 1)
	assertCount(t, reopened, `SELECT COUNT(*) FROM sales_order_items WHERE order_id=?`, orderID, 1)
	assertCount(t, reopened, `SELECT COUNT(*) FROM payments WHERE order_id=? AND status='captured'`, orderID, 1)
	assertCount(t, reopened, `SELECT COUNT(*) FROM receipts WHERE order_id=?`, orderID, 1)
	assertCount(t, reopened, `SELECT COUNT(*) FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=?`, orderID, 1)
	assertCount(t, reopened, `SELECT COUNT(*) FROM outbox_events WHERE ordering_key='sales_order:'||? AND event_type='payment.recorded' AND status='pending'`, orderID, 1)
	assertCount(t, reopened, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='sales_order' AND aggregate_id=? AND event_type='sale.completed' AND status='pending'`, orderID, 1)

	var onHand int64
	if err := reopened.SQL().QueryRow(`SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != -1000 {
		t.Fatalf("on_hand_milli=%d", onHand)
	}

	snap, err := observability.New(reopened, outbox.New(reopened), filepath.Join(t.TempDir(), "backups")).Collect(ctx)
	if err != nil {
		t.Fatalf("collect observability: %v", err)
	}
	if !snap.DatabaseOK || snap.Outbox.Pending != 2 {
		t.Fatalf("unexpected observability snapshot: %#v", snap)
	}
}

func openMigratedDB(t *testing.T, path string) *database.DB {
	t.Helper()
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		db.Close()
		t.Fatalf("migrate database: %v", err)
	}
	if err := db.IntegrityCheck(context.Background()); err != nil {
		db.Close()
		t.Fatalf("integrity check: %v", err)
	}
	return db
}

func newTestServer(db *database.DB, deviceService *device.Service) *Server {
	catalogRepo := catalog.NewRepository(db)
	return New(
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
}

func seedOrderCatalog(t *testing.T, db *database.DB) {
	t.Helper()
	changes := inbox.New(db)
	applyCatalogMessage(t, changes, "product-vertical-1", "catalog.product.upsert", map[string]any{
		"id": "product-1", "name": "Milk", "unit_of_measure": "unit", "is_active": true, "allow_manual_price": false, "track_inventory": true, "version": 1,
	})
	applyCatalogMessage(t, changes, "price-vertical-1", "catalog.price.upsert", map[string]any{
		"id": "price-1", "product_id": "product-1", "store_id": "store-1", "currency": "INR", "amount_minor": 12500, "tax_inclusive": true, "priority": 100, "version": 1,
	})
}

func postOrder(t *testing.T, app *Server) string {
	t.Helper()
	body := map[string]any{
		"client_order_id": "client-order-vertical-1",
		"currency":        "INR",
		"items":           []map[string]any{{"product_id": "product-1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0}},
	}
	res := serveJSON(t, app, http.MethodPost, "/api/v1/orders", body)
	if res.Code != http.StatusCreated {
		t.Fatalf("create order status=%d body=%s", res.Code, res.Body.String())
	}
	var order orders.Order
	if err := json.NewDecoder(res.Body).Decode(&order); err != nil {
		t.Fatal(err)
	}
	if order.ID == "" || len(order.Items) != 1 {
		t.Fatalf("unexpected order: %#v", order)
	}
	return order.ID
}

func postPayment(t *testing.T, app *Server, orderID, clientPaymentID string) {
	body := map[string]any{"client_payment_id": clientPaymentID, "mode": "cash", "amount_minor": 12500, "currency": "INR", "status": "captured"}
	res := serveJSON(t, app, http.MethodPost, "/api/v1/orders/"+orderID+"/payments", body)
	if res.Code != http.StatusCreated {
		t.Fatalf("create payment status=%d body=%s", res.Code, res.Body.String())
	}
}

func completeOrder(t *testing.T, app *Server, orderID string) {
	res := serveJSON(t, app, http.MethodPost, "/api/v1/orders/"+orderID+"/complete", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("complete order status=%d body=%s", res.Code, res.Body.String())
	}
}

func serveJSON(t *testing.T, app *Server, method, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, target, &body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(res, req)
	return res
}

func assertCount(t *testing.T, db *database.DB, query string, arg any, want int) {
	t.Helper()
	var got int
	if err := db.SQL().QueryRow(query, arg).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count mismatch for %q: got %d want %d", query, got, want)
	}
}
