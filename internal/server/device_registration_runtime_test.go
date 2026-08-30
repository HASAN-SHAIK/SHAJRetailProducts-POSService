package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
)

func TestV1DeviceRegistrationRuntimeHTTPPersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")

	db := openMigratedDB(t, dbPath)
	deviceService := device.New(db)
	installed, err := deviceService.EnsureInstallation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Status != "unregistered" {
		t.Fatalf("expected unregistered first-run identity, got %q", installed.Status)
	}

	app := newTestServer(db, deviceService)
	baseURL, stop := startDeviceRegistrationLiveServer(t, app)

	status, body := deviceRegistrationJSON(t, http.MethodPut, baseURL+"/api/v1/device/registration", map[string]any{
		"store_id":      "branch-a",
		"store_number":  "STORE-001",
		"pos_no":        "POS-07",
		"touchpoint_id": "TP-02",
	})
	if status != http.StatusOK {
		t.Fatalf("registration status=%d body=%s", status, body)
	}

	var registered device.Identity
	if err := json.Unmarshal(body, &registered); err != nil {
		t.Fatal(err)
	}
	if registered.Status != "active" || registered.StoreID == nil || *registered.StoreID != "branch-a" || registered.StoreNumber == nil || *registered.StoreNumber != "STORE-001" || registered.POSNo == nil || *registered.POSNo != "POS-07" || registered.TouchpointID == nil || *registered.TouchpointID != "TP-02" {
		t.Fatalf("unexpected registered identity: %#v", registered)
	}

	deviceID := registered.DeviceID
	installationID := registered.InstallationID

	status, body = deviceRegistrationJSON(t, http.MethodGet, baseURL+"/api/v1/device", nil)
	if status != http.StatusOK {
		t.Fatalf("device status=%d body=%s", status, body)
	}
	var beforeRestart device.Identity
	if err := json.Unmarshal(body, &beforeRestart); err != nil {
		t.Fatal(err)
	}
	if beforeRestart.DeviceID != deviceID || beforeRestart.InstallationID != installationID || beforeRestart.Status != "active" {
		t.Fatalf("runtime device response diverged before restart: %#v", beforeRestart)
	}

	stop()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openMigratedDB(t, dbPath)
	defer reopened.Close()
	restartedDeviceService := device.New(reopened)
	if _, err := restartedDeviceService.EnsureInstallation(ctx); err != nil {
		t.Fatal(err)
	}
	restartedApp := newTestServer(reopened, restartedDeviceService)
	restartedURL, restartedStop := startDeviceRegistrationLiveServer(t, restartedApp)
	defer restartedStop()

	status, body = deviceRegistrationJSON(t, http.MethodGet, restartedURL+"/api/v1/device", nil)
	if status != http.StatusOK {
		t.Fatalf("device after restart status=%d body=%s", status, body)
	}
	var afterRestart device.Identity
	if err := json.Unmarshal(body, &afterRestart); err != nil {
		t.Fatal(err)
	}
	if afterRestart.DeviceID != deviceID || afterRestart.InstallationID != installationID || afterRestart.Status != "active" || afterRestart.StoreID == nil || *afterRestart.StoreID != "branch-a" || afterRestart.StoreNumber == nil || *afterRestart.StoreNumber != "STORE-001" || afterRestart.POSNo == nil || *afterRestart.POSNo != "POS-07" || afterRestart.TouchpointID == nil || *afterRestart.TouchpointID != "TP-02" {
		t.Fatalf("registration did not survive real runtime restart: %#v", afterRestart)
	}
}

func TestV1DeviceRegistrationRuntimeHTTPRejectsIncompleteIdentity(t *testing.T) {
	db := openMigratedDB(t, filepath.Join(t.TempDir(), "pos.db"))
	defer db.Close()
	deviceService := device.New(db)
	if _, err := deviceService.EnsureInstallation(context.Background()); err != nil {
		t.Fatal(err)
	}
	app := newTestServer(db, deviceService)
	baseURL, stop := startDeviceRegistrationLiveServer(t, app)
	defer stop()

	status, body := deviceRegistrationJSON(t, http.MethodPut, baseURL+"/api/v1/device/registration", map[string]any{
		"store_id": "branch-a",
		"pos_no":   "POS-01",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected incomplete identity rejection, got status=%d body=%s", status, body)
	}

	status, body = deviceRegistrationJSON(t, http.MethodGet, baseURL+"/api/v1/device", nil)
	if status != http.StatusOK {
		t.Fatalf("device status=%d body=%s", status, body)
	}
	var identity device.Identity
	if err := json.Unmarshal(body, &identity); err != nil {
		t.Fatal(err)
	}
	if identity.Status != "unregistered" || identity.StoreID != nil || identity.StoreNumber != nil || identity.POSNo != nil || identity.TouchpointID != nil {
		t.Fatalf("failed registration mutated persisted identity: %#v", identity)
	}
}

func startDeviceRegistrationLiveServer(t *testing.T, app *Server) (string, func()) {
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
			t.Fatalf("POSService registration runtime did not become healthy: %v", err)
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
			t.Fatalf("shutdown registration runtime: %v", err)
		}
		select {
		case err := <-serverErr:
			if err != nil {
				t.Fatalf("registration runtime stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("registration runtime did not stop")
		}
	}
	return "http://" + addr, stop
}

func deviceRegistrationJSON(t *testing.T, method, url string, payload any) (int, []byte) {
	t.Helper()
	var requestBody bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&requestBody).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, url, &requestBody)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("runtime request %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode runtime response %s %s: %v", method, url, err)
	}
	return resp.StatusCode, []byte(raw)
}
