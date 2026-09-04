package server

import (
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

func TestV1ReadinessTransitionsWhenDeviceIdentityBecomesAvailableOverLiveHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)

	var identityRows int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM device_identity`).Scan(&identityRows); err != nil {
		t.Fatalf("count pre-start device identity rows: %v", err)
	}
	if identityRows != 0 {
		t.Fatalf("precondition device identity rows=%d want=0", identityRows)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	catalogRepo := catalog.NewRepository(db)
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
		if err := app.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown POSService: %v", err)
		}
		select {
		case err := <-serverErr:
			if err != nil {
				t.Errorf("POSService stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("POSService did not stop after shutdown")
		}
	})

	client := &http.Client{Timeout: 2 * time.Second}
	baseURL := "http://" + addr
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
			t.Fatalf("POSService did not become healthy at %s: %v", baseURL, err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	resp, err := client.Get(baseURL + "/api/v1/ready")
	if err != nil {
		t.Fatalf("initial readiness request: %v", err)
	}
	var initial map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&initial); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode initial readiness response: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("initial readiness status=%d want=%d body=%v", resp.StatusCode, http.StatusServiceUnavailable, initial)
	}
	if initial["status"] != "not_ready" || initial["reason"] != "device_identity_unavailable" {
		t.Fatalf("unexpected initial readiness body: %v", initial)
	}

	identity, err := deviceService.EnsureInstallation(ctx)
	if err != nil {
		t.Fatalf("create local device identity while server is running: %v", err)
	}
	if identity.DeviceID == "" || identity.InstallationID == "" {
		t.Fatalf("created incomplete local device identity: %+v", identity)
	}

	resp, err = client.Get(baseURL + "/api/v1/ready")
	if err != nil {
		t.Fatalf("readiness request after device identity creation: %v", err)
	}
	var ready map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ready); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode ready response: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post-identity readiness status=%d want=%d body=%v", resp.StatusCode, http.StatusOK, ready)
	}
	if ready["status"] != "ready" {
		t.Fatalf("unexpected ready response: %v", ready)
	}

	var persistedDeviceID, persistedInstallationID string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT device_id, installation_id
		FROM device_identity
		WHERE singleton_id=1`).Scan(&persistedDeviceID, &persistedInstallationID); err != nil {
		t.Fatalf("read persisted device identity: %v", err)
	}
	if persistedDeviceID != identity.DeviceID || persistedInstallationID != identity.InstallationID {
		t.Fatalf("persisted identity mismatch device=%q/%q installation=%q/%q", persistedDeviceID, identity.DeviceID, persistedInstallationID, identity.InstallationID)
	}
}
