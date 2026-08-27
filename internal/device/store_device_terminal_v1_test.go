package device

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestV1ApprovedPOSBusinessIdentityPersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pos.db")

	db, err := database.Open(ctx, path)
	if err != nil { t.Fatal(err) }
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

	service := New(db)
	installed, err := service.EnsureInstallation(ctx)
	if err != nil { t.Fatal(err) }
	if installed.Status != "unregistered" { t.Fatalf("expected first-run identity to be unregistered, got %q", installed.Status) }

	approved, err := service.ApplyRegistration(ctx, Registration{
		StoreID:      "branch-a",
		StoreNumber:  "STORE-001",
		POSNo:        "POS-07",
		TouchpointID: "TP-02",
	})
	if err != nil { t.Fatal(err) }
	if approved.StoreID == nil || *approved.StoreID != "branch-a" { t.Fatalf("expected approved branch identity, got %+v", approved.StoreID) }
	if approved.StoreNumber == nil || *approved.StoreNumber != "STORE-001" { t.Fatalf("expected store number, got %+v", approved.StoreNumber) }
	if approved.POSNo == nil || *approved.POSNo != "POS-07" { t.Fatalf("expected POS number, got %+v", approved.POSNo) }
	if approved.TouchpointID == nil || *approved.TouchpointID != "TP-02" { t.Fatalf("expected touchpoint identity, got %+v", approved.TouchpointID) }
	if approved.TerminalID == nil || *approved.TerminalID != "POS-07" { t.Fatalf("expected terminal compatibility alias to mirror POS number, got %+v", approved.TerminalID) }
	if approved.Status != "active" { t.Fatalf("expected approved identity to become active, got %q", approved.Status) }

	deviceID := approved.DeviceID
	installationID := approved.InstallationID
	if err := db.Close(); err != nil { t.Fatal(err) }
	db, err = database.Open(ctx, path)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

	restarted, err := New(db).Get(ctx)
	if err != nil { t.Fatal(err) }
	if restarted.DeviceID != deviceID || restarted.InstallationID != installationID {
		t.Fatalf("physical POS identity changed across restart: before=%s/%s after=%s/%s", deviceID, installationID, restarted.DeviceID, restarted.InstallationID)
	}
	if restarted.StoreID == nil || *restarted.StoreID != "branch-a" || restarted.StoreNumber == nil || *restarted.StoreNumber != "STORE-001" || restarted.POSNo == nil || *restarted.POSNo != "POS-07" || restarted.TouchpointID == nil || *restarted.TouchpointID != "TP-02" {
		t.Fatalf("approved Store/POS/Touchpoint identity did not survive restart: %+v", restarted)
	}
	if restarted.Status != "active" { t.Fatalf("expected active registration to survive restart, got %q", restarted.Status) }
}

func TestV1RegistrationRejectsIncompleteBusinessIdentity(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }
	service := New(db)
	if _, err := service.EnsureInstallation(ctx); err != nil { t.Fatal(err) }

	if _, err := service.ApplyRegistration(ctx, Registration{StoreID: "branch-a", POSNo: "POS-01"}); err == nil {
		t.Fatal("expected missing store_number and touchpoint_id to be rejected")
	}
}

func TestV1SeededInstallationIdentitySupportsLocalSimulation(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

	service := New(db)
	installed, err := service.EnsureInstallationWithSeed(ctx, InstallationSeed{DeviceID: "SIM-POS-DTN-01", InstallationID: "SIM-INSTALL-DTN-01"})
	if err != nil { t.Fatal(err) }
	if installed.DeviceID != "SIM-POS-DTN-01" || installed.InstallationID != "SIM-INSTALL-DTN-01" { t.Fatalf("expected seeded identity, got %s/%s", installed.DeviceID, installed.InstallationID) }

	restarted, err := service.EnsureInstallationWithSeed(ctx, InstallationSeed{DeviceID: "SIM-POS-DTN-02", InstallationID: "SIM-INSTALL-DTN-02"})
	if err != nil { t.Fatal(err) }
	if restarted.DeviceID != installed.DeviceID || restarted.InstallationID != installed.InstallationID { t.Fatalf("existing identity changed after restart: before=%s/%s after=%s/%s", installed.DeviceID, installed.InstallationID, restarted.DeviceID, restarted.InstallationID) }
}
