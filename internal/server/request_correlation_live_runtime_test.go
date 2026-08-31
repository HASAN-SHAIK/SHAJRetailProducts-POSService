package server

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
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

func TestV1RequestCorrelationRunsAcrossLivePOSServiceHTTPBoundary(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
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

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previousLogger)

	serverErr := make(chan error, 1)
	go func() { serverErr <- app.Start() }()
	defer func() {
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
	}()

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
	logs.Reset()

	const safeID = "cashier-req_123:retry.1"
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/health?secret=do-not-log", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request-ID", safeID)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("live health request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("live health status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("X-Request-ID"); got != safeID {
		t.Fatalf("safe request ID not preserved at live boundary: got=%q want=%q", got, safeID)
	}

	logLine := logs.String()
	for _, expected := range []string{`"request_id":"` + safeID + `"`, `"method":"GET"`, `"status":200`} {
		if !strings.Contains(logLine, expected) {
			t.Fatalf("live request correlation log missing %s: %s", expected, logLine)
		}
	}
	if strings.Contains(logLine, "secret=do-not-log") {
		t.Fatalf("live request correlation log leaked query string: %s", logLine)
	}

	logs.Reset()
	const unsafeID = "unsafe request id with spaces"
	req, err = http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request-ID", unsafeID)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("live unsafe-ID request: %v", err)
	}
	_ = resp.Body.Close()
	generated := resp.Header.Get("X-Request-ID")
	if generated == "" || generated == unsafeID || !strings.HasPrefix(generated, "pos-") || !isSafeRequestID(generated) {
		t.Fatalf("expected live boundary to replace unsafe caller ID with safe POS ID: supplied=%q generated=%q", unsafeID, generated)
	}
	if !strings.Contains(logs.String(), `"request_id":"`+generated+`"`) {
		t.Fatalf("generated live request ID missing from structured log: id=%q logs=%s", generated, logs.String())
	}
}
