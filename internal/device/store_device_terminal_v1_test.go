package device

import (
    "context"
    "path/filepath"
    "testing"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestV1ApprovedTerminalIdentityPersistsAcrossRestart(t *testing.T) {
    ctx := context.Background()
    path := filepath.Join(t.TempDir(), "pos.db")

    db, err := database.Open(ctx, path)
    if err != nil { t.Fatal(err) }
    if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

    service := New(db)
    installed, err := service.EnsureInstallation(ctx)
    if err != nil { t.Fatal(err) }
    if installed.Status != "unregistered" {
        t.Fatalf("expected first-run identity to be unregistered, got %q", installed.Status)
    }

    approved, err := service.ApplyRegistration(ctx, Registration{
        StoreID: "branch-a",
        TerminalID: "terminal-07",
    })
    if err != nil { t.Fatal(err) }
    if approved.StoreID == nil || *approved.StoreID != "branch-a" {
        t.Fatalf("expected approved branch identity, got %+v", approved.StoreID)
    }
    if approved.TerminalID == nil || *approved.TerminalID != "terminal-07" {
        t.Fatalf("expected approved terminal identity, got %+v", approved.TerminalID)
    }
    if approved.Status != "active" {
        t.Fatalf("expected approved identity to become active, got %q", approved.Status)
    }

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
    if restarted.StoreID == nil || *restarted.StoreID != "branch-a" || restarted.TerminalID == nil || *restarted.TerminalID != "terminal-07" {
        t.Fatalf("approved store/terminal identity did not survive restart: %+v", restarted)
    }
    if restarted.Status != "active" {
        t.Fatalf("expected active registration to survive restart, got %q", restarted.Status)
    }
}
