package changefeed

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
)

func TestRealCentralCustomerReconciliationE2E(t *testing.T) {
    centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
    if centralURL == "" { t.Skip("POS_E2E_CENTRAL_URL is not set") }

    ctx := context.Background()
    db, err := database.Open(ctx, filepath.Join(t.TempDir(), "central-customer-reconcile.db"))
    if err != nil { t.Fatal(err) }
    defer func() { _ = db.Close() }()
    if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

    const mappedLocalID = "cus-central-reconcile-e2e"
    now := time.Now().UTC().Format(time.RFC3339Nano)
    if _, err := db.SQL().ExecContext(ctx, `INSERT INTO customers(
        id,name,phone,email,tax_id,credit_limit_minor,outstanding_minor,currency,status,
        source_updated_at,created_at,updated_at,local_version,sync_state
      ) VALUES(?,?,?,?,?,990000,0,'INR','active',NULL,?,?,1,'pending')`,
        mappedLocalID, "Offline Before Central", "8111111111", "offline@example.test", "GST-OFFLINE", now, now); err != nil {
        t.Fatalf("seed mapped offline customer: %v", err)
    }

    inboxService := inbox.New(db)
    puller := New(
        db,
        inboxService,
        centralURL,
        envCustomer("POS_E2E_TENANT_ID", "tenant-e2e"),
        envCustomer("POS_E2E_SYNC_TOKEN", "sync-secret"),
        envCustomer("POS_E2E_DEVICE_ID", "device-e2e"),
        5*time.Second,
        50*time.Millisecond,
    )

    runCtx, cancel := context.WithCancel(ctx)
    defer cancel()
    go puller.Run(runCtx)

    repo := customer.NewRepository(db)
    deadline := time.Now().Add(8 * time.Second)
    var projected customer.Customer
    for time.Now().Before(deadline) {
        projected, err = repo.Get(ctx, mappedLocalID)
        if err == nil && projected.SyncState == "synced" && projected.Name == "Central Reconciled Customer" {
            break
        }
        time.Sleep(50 * time.Millisecond)
    }
    if err != nil { t.Fatalf("read reconciled customer: %v", err) }
    if projected.Name != "Central Reconciled Customer" || projected.SyncState != "synced" {
        t.Fatalf("Central customer did not reconcile in time: %+v", projected)
    }
    if projected.Email == nil || *projected.Email != "central-reconciled@example.test" { t.Fatalf("email = %v", projected.Email) }
    if projected.TaxID == nil || *projected.TaxID != "GST-CENTRAL-RECONCILED" { t.Fatalf("tax id = %v", projected.TaxID) }
    if projected.CreditLimitMinor != 12345 || projected.OutstandingMinor != 6789 { t.Fatalf("Central financial snapshots = %d/%d", projected.CreditLimitMinor, projected.OutstandingMinor) }
    if projected.Status != "inactive" { t.Fatalf("status = %s, want inactive", projected.Status) }

    var mappedRows, canonicalDuplicate int
    if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM customers WHERE id=?`, mappedLocalID).Scan(&mappedRows); err != nil { t.Fatal(err) }
    if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM customers WHERE id='42'`).Scan(&canonicalDuplicate); err != nil { t.Fatal(err) }
    if mappedRows != 1 || canonicalDuplicate != 0 {
        t.Fatalf("identity reconciliation rows mapped=%d canonical_duplicate=%d", mappedRows, canonicalDuplicate)
    }
}

func envCustomer(key, fallback string) string {
    if value := os.Getenv(key); value != "" { return value }
    return fallback
}
