package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
)

func TestRegisteredDeviceIdentitySurvivesLivePOSServiceRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")

	db := openDeviceRegistrationRestartDB(t, dbPath)
	deviceService := device.New(db)
	installed, err := deviceService.EnsureInstallation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := deviceService.ApplyRegistration(ctx, device.Registration{
		StoreID:      "store-restart",
		StoreNumber:  "STORE-RESTART-01",
		POSNo:        "POS-RESTART-07",
		TouchpointID: "TP-RESTART-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if installed.DeviceID != registered.DeviceID || installed.InstallationID != registered.InstallationID {
		t.Fatalf("registration changed physical identity: installed=%s/%s registered=%s/%s", installed.DeviceID, installed.InstallationID, registered.DeviceID, registered.InstallationID)
	}

	app1, baseURL1, stop1 := startDeviceRegistrationRestartServer(t, db, deviceService)
	before := getDeviceRegistrationRestartIdentity(t, baseURL1)
	assertDeviceRegistrationRestartIdentity(t, before, registered)
	stop1()
	if err := db.Close(); err != nil {
		t.Fatalf("close first sqlite handle: %v", err)
	}

	reopened := openDeviceRegistrationRestartDB(t, dbPath)
	deviceService2 := device.New(reopened)
	ensuredAfterRestart, err := deviceService2.EnsureInstallation(ctx)
	if err != nil {
		t.Fatalf("ensure installation after restart: %v", err)
	}
	assertDeviceRegistrationRestartIdentity(t, ensuredAfterRestart, registered)

	app2, baseURL2, stop2 := startDeviceRegistrationRestartServer(t, reopened, deviceService2)
	after := getDeviceRegistrationRestartIdentity(t, baseURL2)
	assertDeviceRegistrationRestartIdentity(t, after, registered)

	var deviceID, installationID, storeID, storeNumber, posNo, touchpointID, terminalID, status, registeredAt string
	var rowCount int
	if err := reopened.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*), device_id, installation_id, store_id, store_number, pos_no,
		       touchpoint_id, terminal_id, status, registered_at
		FROM device_identity WHERE singleton_id=1`).Scan(
		&rowCount, &deviceID, &installationID, &storeID, &storeNumber, &posNo,
		&touchpointID, &terminalID, &status, &registeredAt,
	); err != nil {
		t.Fatalf("read persisted device identity after restart: %v", err)
	}
	if rowCount != 1 || deviceID != registered.DeviceID || installationID != registered.InstallationID ||
		storeID != "store-restart" || storeNumber != "STORE-RESTART-01" || posNo != "POS-RESTART-07" ||
		touchpointID != "TP-RESTART-02" || terminalID != "POS-RESTART-07" || status != "active" ||
		registered.RegisteredAt == nil || registeredAt != *registered.RegisteredAt {
		t.Fatalf("persisted registration diverged after restart: count=%d device=%q installation=%q store=%q/%q pos=%q touchpoint=%q terminal=%q status=%q registered_at=%q",
			rowCount, deviceID, installationID, storeID, storeNumber, posNo, touchpointID, terminalID, status, registeredAt)
	}

	stop2()
	if err := reopened.Close(); err != nil {
		t.Fatalf("close restarted sqlite handle: %v", err)
	}
	_ = app1
	_ = app2
}

func openDeviceRegistrationRestartDB(t *testing.T, path string) *database.DB {
	t.Helper()
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open sqlite %s: %v", path, err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("migrate sqlite %s: %v", path, err)
	}
	return db
}

func startDeviceRegistrationRestartServer(t *testing.T, db *database.DB, deviceService *device.Service) (*Server, string, func()) {
	t.Helper()
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

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get("http://" + addr + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("POSService did not become healthy at %s: %v", addr, err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := app.Shutdown(shutdownCtx); err != nil {
			t.Fatalf("shutdown POSService: %v", err)
		}
		select {
		case err := <-serverErr:
			if err != nil {
				t.Fatalf("POSService stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("POSService did not stop after shutdown")
		}
	}
	return app, "http://" + addr, stop
}

func getDeviceRegistrationRestartIdentity(t *testing.T, baseURL string) device.Identity {
	t.Helper()
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(baseURL + "/api/v1/device")
	if err != nil {
		t.Fatalf("GET /api/v1/device: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/device status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	var identity device.Identity
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		t.Fatalf("decode device identity: %v", err)
	}
	return identity
}

func assertDeviceRegistrationRestartIdentity(t *testing.T, got, want device.Identity) {
	t.Helper()
	if got.DeviceID != want.DeviceID || got.InstallationID != want.InstallationID || got.Status != "active" {
		t.Fatalf("physical device identity/status changed: got=%+v want=%+v", got, want)
	}
	if got.StoreID == nil || want.StoreID == nil || *got.StoreID != *want.StoreID ||
		got.StoreNumber == nil || want.StoreNumber == nil || *got.StoreNumber != *want.StoreNumber ||
		got.POSNo == nil || want.POSNo == nil || *got.POSNo != *want.POSNo ||
		got.TouchpointID == nil || want.TouchpointID == nil || *got.TouchpointID != *want.TouchpointID ||
		got.TerminalID == nil || want.TerminalID == nil || *got.TerminalID != *want.TerminalID {
		t.Fatalf("registered business identity changed: got=%+v want=%+v", got, want)
	}
	if got.RegisteredAt == nil || want.RegisteredAt == nil || *got.RegisteredAt != *want.RegisteredAt {
		t.Fatalf("registered_at changed: got=%v want=%v", got.RegisteredAt, want.RegisteredAt)
	}
}
