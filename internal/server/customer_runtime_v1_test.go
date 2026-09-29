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

func TestV1CustomerLifecycleOverLivePOSServiceHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	defer db.Close()

	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-1", StoreNumber: "STORE-001", POSNo: "POS-01", TouchpointID: "TP-01"}); err != nil {
		t.Fatal(err)
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
	baseURL := startCustomerLiveRuntime(t, app)

	createdRaw := customerRuntimeJSON(t, http.MethodPost, baseURL+"/api/v1/customers", map[string]any{
		"customer_code":      "CUST-001",
		"name":               "Ayesha Khan",
		"phone":              "9876543210",
		"email":              "ayesha@example.test",
		"credit_limit_minor": 250000,
		"currency":           "INR",
	}, http.StatusCreated)
	var created customer.Customer
	if err := json.Unmarshal(createdRaw, &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Ayesha Khan" || created.LocalVersion != 1 || created.SyncState != "pending" {
		t.Fatalf("unexpected created customer: %#v", created)
	}

	getRaw := customerRuntimeJSON(t, http.MethodGet, baseURL+"/api/v1/customers/"+created.ID, nil, http.StatusOK)
	var got customer.Customer
	if err := json.Unmarshal(getRaw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.Name != created.Name || got.CreditLimitMinor != 250000 {
		t.Fatalf("unexpected customer GET: %#v", got)
	}

	searchRaw := customerRuntimeJSON(t, http.MethodGet, baseURL+"/api/v1/customers?q=ayesha&limit=10", nil, http.StatusOK)
	var search struct {
		Items []customer.Customer `json:"items"`
		Count int                 `json:"count"`
	}
	if err := json.Unmarshal(searchRaw, &search); err != nil {
		t.Fatal(err)
	}
	if search.Count != 1 || len(search.Items) != 1 || search.Items[0].ID != created.ID {
		t.Fatalf("unexpected customer search: %#v", search)
	}

	updatedRaw := customerRuntimeJSON(t, http.MethodPut, baseURL+"/api/v1/customers/"+created.ID, map[string]any{
		"customer_code":      "CUST-001",
		"name":               "Ayesha K",
		"phone":              "9876543210",
		"email":              "ayesha.k@example.test",
		"credit_limit_minor": 300000,
		"currency":           "INR",
	}, http.StatusOK)
	var updated customer.Customer
	if err := json.Unmarshal(updatedRaw, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Name != "Ayesha K" || updated.CreditLimitMinor != 300000 || updated.LocalVersion != 2 || updated.SyncState != "pending" {
		t.Fatalf("unexpected updated customer: %#v", updated)
	}

	var name, email, syncState string
	var creditLimit, localVersion int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT name,email,credit_limit_minor,local_version,sync_state FROM customers WHERE id=?`, created.ID).Scan(&name, &email, &creditLimit, &localVersion, &syncState); err != nil {
		t.Fatal(err)
	}
	if name != "Ayesha K" || email != "ayesha.k@example.test" || creditLimit != 300000 || localVersion != 2 || syncState != "pending" {
		t.Fatalf("persisted customer mismatch name=%q email=%q credit=%d version=%d sync=%q", name, email, creditLimit, localVersion, syncState)
	}

	var outboxCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='customer' AND aggregate_id=? AND event_type='customer.changed' AND status='pending'`, created.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if outboxCount != 2 {
		t.Fatalf("customer.changed pending outbox count=%d want=2", outboxCount)
	}

	var versions string
	if err := db.SQL().QueryRowContext(ctx, `SELECT group_concat(aggregate_version, ',') FROM (SELECT aggregate_version FROM outbox_events WHERE aggregate_type='customer' AND aggregate_id=? AND event_type='customer.changed' ORDER BY aggregate_version)`, created.ID).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != "1,2" {
		t.Fatalf("customer.changed versions=%q want=1,2", versions)
	}
}

func startCustomerLiveRuntime(t *testing.T, app *Server) string {
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
				t.Errorf("customer runtime shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("customer runtime did not stop")
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
			t.Fatalf("customer POSService runtime did not become healthy: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func customerRuntimeJSON(t *testing.T, method, url string, body any, wantStatus int) []byte {
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
