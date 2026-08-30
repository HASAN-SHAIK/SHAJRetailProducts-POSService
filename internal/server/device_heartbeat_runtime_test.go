package server

import (
	"context"
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

func TestDeviceHeartbeatPersistsOverLiveHTTPServer(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceService.ApplyRegistration(ctx, device.Registration{
		StoreID:      "store-1",
		StoreNumber:  "STORE-001",
		POSNo:        "POS-01",
		TouchpointID: "TP-01",
	}); err != nil {
		t.Fatal(err)
	}

	before, err := deviceService.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.LastHeartbeatAt != nil {
		t.Fatalf("precondition last_heartbeat_at=%q want nil", *before.LastHeartbeatAt)
	}
	beforeUpdatedAt := before.UpdatedAt

	catalogRepo := catalog.NewRepository(db)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	app := New(
		config.Config{Environment: "test", ListenAddress: addr},
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
			t.Fatalf("live POSService did not become healthy: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	requestStartedAt := time.Now().UTC().Add(-time.Second)
	resp, err := client.Post(baseURL+"/api/v1/device/heartbeat", "application/json", nil)
	if err != nil {
		t.Fatalf("heartbeat request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("heartbeat status=%d want=%d", resp.StatusCode, http.StatusNoContent)
	}

	after, err := deviceService.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.LastHeartbeatAt == nil {
		t.Fatal("last_heartbeat_at remained nil after live heartbeat")
	}
	heartbeatAt, err := time.Parse(time.RFC3339Nano, *after.LastHeartbeatAt)
	if err != nil {
		t.Fatalf("parse heartbeat timestamp %q: %v", *after.LastHeartbeatAt, err)
	}
	if heartbeatAt.Before(requestStartedAt) {
		t.Fatalf("heartbeat timestamp=%s predates request start=%s", heartbeatAt, requestStartedAt)
	}
	if after.UpdatedAt == beforeUpdatedAt {
		t.Fatalf("updated_at did not change: %s", after.UpdatedAt)
	}

	var persistedHeartbeat, persistedUpdatedAt string
	if err := db.SQL().QueryRow(`SELECT last_heartbeat_at, updated_at FROM device_identity WHERE singleton_id=1`).Scan(&persistedHeartbeat, &persistedUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if persistedHeartbeat != *after.LastHeartbeatAt {
		t.Fatalf("sqlite last_heartbeat_at=%q want=%q", persistedHeartbeat, *after.LastHeartbeatAt)
	}
	if persistedUpdatedAt != after.UpdatedAt {
		t.Fatalf("sqlite updated_at=%q want=%q", persistedUpdatedAt, after.UpdatedAt)
	}

	resp, err = client.Get(baseURL + "/api/v1/device")
	if err != nil {
		t.Fatalf("device readback request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device readback status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
}
