package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

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
	registerRes := serveJSON(t, app, http.MethodPut, "/api/v1/device/registration", map[string]any{
		"store_id":      "branch-a",
		"store_number":  "STORE-001",
		"pos_no":        "POS-07",
		"touchpoint_id": "TP-02",
	})
	if registerRes.Code != http.StatusOK {
		t.Fatalf("registration status=%d body=%s", registerRes.Code, registerRes.Body.String())
	}

	var registered device.Identity
	if err := json.NewDecoder(registerRes.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	if registered.Status != "active" || registered.StoreID == nil || *registered.StoreID != "branch-a" || registered.StoreNumber == nil || *registered.StoreNumber != "STORE-001" || registered.POSNo == nil || *registered.POSNo != "POS-07" || registered.TouchpointID == nil || *registered.TouchpointID != "TP-02" {
		t.Fatalf("unexpected registered identity: %#v", registered)
	}

	deviceID := registered.DeviceID
	installationID := registered.InstallationID

	getRes := serveJSON(t, app, http.MethodGet, "/api/v1/device", nil)
	if getRes.Code != http.StatusOK {
		t.Fatalf("device status=%d body=%s", getRes.Code, getRes.Body.String())
	}
	var beforeRestart device.Identity
	if err := json.NewDecoder(getRes.Body).Decode(&beforeRestart); err != nil {
		t.Fatal(err)
	}
	if beforeRestart.DeviceID != deviceID || beforeRestart.InstallationID != installationID || beforeRestart.Status != "active" {
		t.Fatalf("runtime device response diverged before restart: %#v", beforeRestart)
	}

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

	restartRes := serveJSON(t, restartedApp, http.MethodGet, "/api/v1/device", nil)
	if restartRes.Code != http.StatusOK {
		t.Fatalf("device after restart status=%d body=%s", restartRes.Code, restartRes.Body.String())
	}
	var afterRestart device.Identity
	if err := json.NewDecoder(restartRes.Body).Decode(&afterRestart); err != nil {
		t.Fatal(err)
	}
	if afterRestart.DeviceID != deviceID || afterRestart.InstallationID != installationID || afterRestart.Status != "active" || afterRestart.StoreID == nil || *afterRestart.StoreID != "branch-a" || afterRestart.StoreNumber == nil || *afterRestart.StoreNumber != "STORE-001" || afterRestart.POSNo == nil || *afterRestart.POSNo != "POS-07" || afterRestart.TouchpointID == nil || *afterRestart.TouchpointID != "TP-02" {
		t.Fatalf("registration did not survive runtime restart path: %#v", afterRestart)
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

	res := serveJSON(t, app, http.MethodPut, "/api/v1/device/registration", map[string]any{
		"store_id": "branch-a",
		"pos_no":   "POS-01",
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected incomplete identity rejection, got status=%d body=%s", res.Code, res.Body.String())
	}

	var identity device.Identity
	getRes := serveJSON(t, app, http.MethodGet, "/api/v1/device", nil)
	if getRes.Code != http.StatusOK {
		t.Fatalf("device status=%d body=%s", getRes.Code, getRes.Body.String())
	}
	if err := json.NewDecoder(getRes.Body).Decode(&identity); err != nil {
		t.Fatal(err)
	}
	if identity.Status != "unregistered" || identity.StoreID != nil || identity.StoreNumber != nil || identity.POSNo != nil || identity.TouchpointID != nil {
		t.Fatalf("failed registration mutated persisted identity: %#v", identity)
	}
}
