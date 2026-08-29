package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
)

func TestV1HealthEndpointReportsRunningPOSService(t *testing.T) {
	startedAt := time.Now().UTC().Add(-2 * time.Second)
	s := &Server{
		cfg:       config.Config{Environment: "test"},
		startedAt: startedAt,
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	s.handleHealth(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("health content type = %q, want application/json", contentType)
	}

	var payload struct {
		Status      string    `json:"status"`
		Service     string    `json:"service"`
		Environment string    `json:"environment"`
		StartedAt   time.Time `json:"started_at"`
		UptimeMS    int64     `json:"uptime_ms"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("health status payload = %q, want ok", payload.Status)
	}
	if payload.Service != "shajretail-pos-service" {
		t.Fatalf("health service = %q, want shajretail-pos-service", payload.Service)
	}
	if payload.Environment != "test" {
		t.Fatalf("health environment = %q, want test", payload.Environment)
	}
	if payload.StartedAt.IsZero() {
		t.Fatal("health response must include started_at")
	}
	if payload.UptimeMS <= 0 {
		t.Fatalf("health uptime_ms = %d, want positive", payload.UptimeMS)
	}
}

func TestV1ReadyEndpointRequiresDatabaseAndDeviceIdentity(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite database: %v", err)
	}

	deviceService := device.New(db)
	s := &Server{db: db, device: deviceService}

	t.Run("identity missing", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
		s.handleReady(recorder, request)

		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("ready status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
		}
		assertJSONReason(t, recorder, "device_identity_unavailable")
	})

	if _, err := deviceService.EnsureInstallationWithSeed(ctx, device.InstallationSeed{
		DeviceID:       "dev_test_startup_health",
		InstallationID: "install_test_startup_health",
	}); err != nil {
		t.Fatalf("ensure test device identity: %v", err)
	}

	t.Run("database and identity ready", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
		s.handleReady(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("ready status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		assertJSONStatus(t, recorder, "ready")
	})
}

func TestV1ReadyEndpointFailsClosedWhenSQLiteIsUnavailable(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite database: %v", err)
	}
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallationWithSeed(ctx, device.InstallationSeed{
		DeviceID:       "dev_test_closed_db",
		InstallationID: "install_test_closed_db",
	}); err != nil {
		t.Fatalf("ensure test device identity: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite database: %v", err)
	}

	s := &Server{db: db, device: deviceService}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	s.handleReady(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	assertJSONReason(t, recorder, "database_unavailable")
}

func assertJSONReason(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	var payload struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
	if payload.Status != "not_ready" {
		t.Fatalf("status = %q, want not_ready", payload.Status)
	}
	if payload.Reason != want {
		t.Fatalf("reason = %q, want %q", payload.Reason, want)
	}
}

func assertJSONStatus(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
	if payload.Status != want {
		t.Fatalf("status = %q, want %q", payload.Status, want)
	}
}
