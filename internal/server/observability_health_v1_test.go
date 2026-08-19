package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func decodeJSONMap(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response JSON: %v", err)
	}
	return payload
}

func TestV1POSHealthIsMinimalAndIndependentFromDatabaseReadiness(t *testing.T) {
	server := &Server{
		cfg:       config.Config{Environment: "test"},
		startedAt: time.Now().UTC().Add(-time.Second),
	}

	recorder := httptest.NewRecorder()
	server.handleHealth(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected liveness 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	payload := decodeJSONMap(t, recorder)
	if payload["status"] != "ok" || payload["service"] != "shajretail-pos-service" || payload["environment"] != "test" {
		t.Fatalf("unexpected liveness payload: %+v", payload)
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"token", "password", "secret", "database_path", "central_api"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("liveness leaked sensitive/config detail %q: %s", forbidden, body)
		}
	}
}

func TestV1POSReadinessRequiresSQLiteAndDurableDeviceIdentity(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	server := &Server{db: db, device: deviceService}

	missingDevice := httptest.NewRecorder()
	server.handleReady(missingDevice, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	if missingDevice.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected missing device identity to fail readiness, got %d: %s", missingDevice.Code, missingDevice.Body.String())
	}
	if payload := decodeJSONMap(t, missingDevice); payload["reason"] != "device_identity_unavailable" {
		t.Fatalf("unexpected missing-device readiness payload: %+v", payload)
	}

	if _, err := deviceService.EnsureInstallation(ctx); err != nil {
		t.Fatalf("ensure durable device identity: %v", err)
	}
	ready := httptest.NewRecorder()
	server.handleReady(ready, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("expected ready POS after database/device initialization, got %d: %s", ready.Code, ready.Body.String())
	}
	if payload := decodeJSONMap(t, ready); payload["status"] != "ready" {
		t.Fatalf("unexpected ready payload: %+v", payload)
	}
}

func TestV1POSReadinessFailsSecretFreeWhenSQLiteIsUnavailable(t *testing.T) {
	db := testutil.OpenDatabase(t)
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(context.Background()); err != nil {
		t.Fatalf("ensure device identity: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database for readiness failure: %v", err)
	}

	server := &Server{db: db, device: deviceService}
	recorder := httptest.NewRecorder()
	server.handleReady(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unavailable SQLite to fail readiness, got %d: %s", recorder.Code, recorder.Body.String())
	}
	payload := decodeJSONMap(t, recorder)
	if payload["status"] != "not_ready" || payload["reason"] != "database_unavailable" {
		t.Fatalf("unexpected unavailable-database readiness payload: %+v", payload)
	}
	body := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{"password", "secret", "sqlite", "file:", "database_path"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("readiness leaked detail %q: %s", forbidden, recorder.Body.String())
		}
	}
}
