package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/syncengine"
)

func TestCompletedSaleOutboxDrainsToCentralAfterLivePOSRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")

	db := openMigratedDB(t, dbPath)
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
	seedOrderCatalog(t, db)

	first := newTestServer(db, deviceService)
	firstBaseURL, stopFirst := startLivePOSForOutboxTest(t, first)

	orderID := liveCreateOrderForOutboxTest(t, firstBaseURL)
	livePostForOutboxTest(t, firstBaseURL+"/api/v1/orders/"+orderID+"/payments", map[string]any{
		"client_payment_id": "client-payment-restart-drain-1",
		"mode":              "cash",
		"amount_minor":      12500,
		"currency":          "INR",
		"status":            "captured",
	}, http.StatusCreated)
	livePostForOutboxTest(t, firstBaseURL+"/api/v1/orders/"+orderID+"/complete", nil, http.StatusOK)

	var pendingBefore int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE status='pending'`).Scan(&pendingBefore); err != nil {
		t.Fatal(err)
	}
	if pendingBefore != 4 {
		t.Fatalf("pending outbox before restart=%d, want 4", pendingBefore)
	}

	stopFirst()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openMigratedDB(t, dbPath)
	defer reopened.Close()
	reopenedDevice := device.New(reopened)
	if _, err := reopenedDevice.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	second := newTestServer(reopened, reopenedDevice)
	secondBaseURL, stopSecond := startLivePOSForOutboxTest(t, second)
	defer stopSecond()

	resp, err := http.Get(secondBaseURL + "/api/v1/orders/" + orderID)
	if err != nil {
		t.Fatalf("read completed order after restart: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("completed order after restart status=%d", resp.StatusCode)
	}

	var mu sync.Mutex
	receivedTypes := make([]string, 0, 4)
	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync/events" {
			t.Errorf("central path=%s", r.URL.Path)
		}
		if got := r.Header.Get("X-POS-Tenant-ID"); got != "tenant-1" {
			t.Errorf("tenant header=%q", got)
		}
		if got := r.Header.Get("X-POS-Device-ID"); got != identity.DeviceID {
			t.Errorf("device header=%q want=%q", got, identity.DeviceID)
		}
		if got := r.Header.Get("X-POS-Sync-Token"); got != "sync-secret" {
			t.Errorf("sync token header=%q", got)
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing Idempotency-Key")
		}

		var envelope syncengine.Envelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode Central envelope: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		receivedTypes = append(receivedTypes, envelope.EventType)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer central.Close()

	engine, err := syncengine.New(outbox.New(reopened), central.URL, "tenant-1", "sync-secret", identity.DeviceID, 2*time.Second, 25*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	published, err := engine.DispatchReady(ctx, 10)
	if err != nil {
		t.Fatalf("dispatch durable sale outbox after restart: %v", err)
	}
	if published != 4 {
		t.Fatalf("published=%d, want 4", published)
	}

	var publishedRows, pendingRows, failedRows int
	if err := reopened.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE status='published'`).Scan(&publishedRows); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE status='pending'`).Scan(&pendingRows); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE status='failed'`).Scan(&failedRows); err != nil {
		t.Fatal(err)
	}
	if publishedRows != 4 || pendingRows != 0 || failedRows != 0 {
		t.Fatalf("outbox after drain published=%d pending=%d failed=%d", publishedRows, pendingRows, failedRows)
	}

	mu.Lock()
	gotTypes := append([]string(nil), receivedTypes...)
	mu.Unlock()
	sort.Strings(gotTypes)
	wantTypes := []string{"inventory.movement.recorded", "payment.recorded", "receipt.issued", "sale.completed"}
	sort.Strings(wantTypes)
	if fmt.Sprint(gotTypes) != fmt.Sprint(wantTypes) {
		t.Fatalf("Central received event types=%v, want %v", gotTypes, wantTypes)
	}
}

func startLivePOSForOutboxTest(t *testing.T, app *Server) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- app.httpServer.Serve(listener)
	}()

	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Errorf("shutdown live POS: %v", err)
		}
		select {
		case err := <-serveDone:
			if err != nil && err != http.ErrServerClosed {
				t.Errorf("serve live POS: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("live POS server did not stop")
		}
	}
	return "http://" + listener.Addr().String(), stop
}

func liveCreateOrderForOutboxTest(t *testing.T, baseURL string) string {
	t.Helper()
	payload := map[string]any{
		"client_order_id": "client-order-restart-drain-1",
		"currency":        "INR",
		"items": []map[string]any{{
			"product_id": "product-1", "quantity_milli": 1000, "discount_minor": 0, "tax_minor": 0,
		}},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/api/v1/orders", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create order over live HTTP: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create order status=%d", resp.StatusCode)
	}
	var order struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		t.Fatal(err)
	}
	if order.ID == "" {
		t.Fatal("created order id is empty")
	}
	return order.ID
}

func livePostForOutboxTest(t *testing.T, url string, payload any, wantStatus int) {
	t.Helper()
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s status=%d want=%d", url, resp.StatusCode, wantStatus)
	}
}
