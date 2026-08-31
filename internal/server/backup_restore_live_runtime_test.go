package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/backup"
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

func TestVerifiedBackupRestoresIntoLivePOSService(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	livePath := filepath.Join(root, "live", "pos.db")
	backupDir := filepath.Join(root, "backups")

	live := openBackupRestoreRuntimeDB(t, livePath)
	deviceService := device.New(live)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatalf("ensure live installation: %v", err)
	}
	registered, err := deviceService.ApplyRegistration(ctx, device.Registration{
		StoreID:      "store-backup-restore",
		StoreNumber:  "STORE-BACKUP-01",
		POSNo:        "POS-BACKUP-07",
		TouchpointID: "TP-BACKUP-02",
	})
	if err != nil {
		t.Fatalf("register live POS identity: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := live.SQL().ExecContext(ctx, `INSERT INTO outbox_events(
		id, aggregate_type, aggregate_id, aggregate_version, event_type, schema_version,
		ordering_key, payload_json, metadata_json, status, attempt_count, available_at, created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evt-backup-live-runtime", "order", "order-backup-live-runtime", 1, "sale.completed", 1,
		"order:order-backup-live-runtime", `{"order_id":"order-backup-live-runtime"}`, `{}`, "pending", 0, now, now,
	); err != nil {
		t.Fatalf("seed durable pending outbox event: %v", err)
	}

	snapshot, err := backup.New(live, backupDir, 3).Create(ctx)
	if err != nil {
		t.Fatalf("create verified backup: %v", err)
	}
	if err := live.Close(); err != nil {
		t.Fatalf("close live database before restore: %v", err)
	}
	if err := backup.ValidateRestoreCandidate(ctx, snapshot.Path); err != nil {
		t.Fatalf("validate restore candidate: %v", err)
	}

	restoredPath := filepath.Join(root, "restored", "pos.db")
	if err := os.MkdirAll(filepath.Dir(restoredPath), 0o750); err != nil {
		t.Fatalf("create restored database directory: %v", err)
	}
	raw, err := os.ReadFile(snapshot.Path)
	if err != nil {
		t.Fatalf("read verified backup: %v", err)
	}
	if err := os.WriteFile(restoredPath, raw, 0o600); err != nil {
		t.Fatalf("install restored database: %v", err)
	}

	restored := openBackupRestoreRuntimeDB(t, restoredPath)
	defer restored.Close()
	if err := restored.IntegrityCheck(ctx); err != nil {
		t.Fatalf("restored database integrity: %v", err)
	}
	restoredDeviceService := device.New(restored)
	restoredIdentity, err := restoredDeviceService.EnsureInstallation(ctx)
	if err != nil {
		t.Fatalf("initialize restored installation: %v", err)
	}
	assertBackupRestoreIdentity(t, restoredIdentity, registered)

	app, baseURL, stop := startBackupRestoreRuntimeServer(t, restored, restoredDeviceService)
	defer stop()
	_ = app

	client := &http.Client{Timeout: 2 * time.Second}
	ready, err := client.Get(baseURL + "/api/v1/ready")
	if err != nil {
		t.Fatalf("GET restored /api/v1/ready: %v", err)
	}
	defer ready.Body.Close()
	if ready.StatusCode != http.StatusOK {
		t.Fatalf("restored /api/v1/ready status=%d want=%d", ready.StatusCode, http.StatusOK)
	}
	var readiness map[string]any
	if err := json.NewDecoder(ready.Body).Decode(&readiness); err != nil {
		t.Fatalf("decode restored readiness: %v", err)
	}
	if readiness["status"] != "ready" {
		t.Fatalf("restored readiness=%v", readiness)
	}

	resp, err := client.Get(baseURL + "/api/v1/device")
	if err != nil {
		t.Fatalf("GET restored /api/v1/device: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restored /api/v1/device status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	var liveIdentity device.Identity
	if err := json.NewDecoder(resp.Body).Decode(&liveIdentity); err != nil {
		t.Fatalf("decode restored live device identity: %v", err)
	}
	assertBackupRestoreIdentity(t, liveIdentity, registered)

	diagnostics, err := client.Get(baseURL + "/api/v1/diagnostics")
	if err != nil {
		t.Fatalf("GET restored /api/v1/diagnostics: %v", err)
	}
	defer diagnostics.Body.Close()
	if diagnostics.StatusCode != http.StatusOK {
		t.Fatalf("restored /api/v1/diagnostics status=%d want=%d", diagnostics.StatusCode, http.StatusOK)
	}

	var pending int
	if err := restored.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE id='evt-backup-live-runtime' AND status='pending'`).Scan(&pending); err != nil {
		t.Fatalf("read restored durable outbox event: %v", err)
	}
	if pending != 1 {
		t.Fatalf("restored pending outbox count=%d want=1", pending)
	}
}

func openBackupRestoreRuntimeDB(t *testing.T, path string) *database.DB {
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

func startBackupRestoreRuntimeServer(t *testing.T, db *database.DB, deviceService *device.Service) (*Server, string, func()) {
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
			t.Fatalf("restored POSService did not become healthy at %s: %v", addr, err)
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
			t.Fatalf("shutdown restored POSService: %v", err)
		}
		select {
		case err := <-serverErr:
			if err != nil {
				t.Fatalf("restored POSService stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("restored POSService did not stop after shutdown")
		}
	}
	return app, "http://" + addr, stop
}

func assertBackupRestoreIdentity(t *testing.T, got, want device.Identity) {
	t.Helper()
	if got.DeviceID != want.DeviceID || got.InstallationID != want.InstallationID || got.Status != "active" {
		t.Fatalf("restored physical identity changed: got=%+v want=%+v", got, want)
	}
	if got.StoreID == nil || want.StoreID == nil || *got.StoreID != *want.StoreID ||
		got.StoreNumber == nil || want.StoreNumber == nil || *got.StoreNumber != *want.StoreNumber ||
		got.POSNo == nil || want.POSNo == nil || *got.POSNo != *want.POSNo ||
		got.TouchpointID == nil || want.TouchpointID == nil || *got.TouchpointID != *want.TouchpointID ||
		got.TerminalID == nil || want.TerminalID == nil || *got.TerminalID != *want.TerminalID {
		t.Fatalf("restored registered business identity changed: got=%+v want=%+v", got, want)
	}
}
