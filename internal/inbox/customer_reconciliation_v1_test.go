package inbox

import (
    "context"
    "encoding/json"
    "testing"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func customerUpsertMessage(t *testing.T, id string, canonicalID string, mappedID string, sourceVersion int64, name string, email string, taxID string, creditLimit int64, outstanding int64, status string) Message {
    t.Helper()
    mappings := []map[string]any{}
    if mappedID != "" {
        mappings = append(mappings, map[string]any{"id": mappedID, "source_version": sourceVersion})
    }
    payload, err := json.Marshal(map[string]any{
        "id": id,
        "canonical_id": canonicalID,
        "pos_mappings": mappings,
        "name": name,
        "phone": "9000000000",
        "email": email,
        "tax_id": taxID,
        "credit_limit_minor": creditLimit,
        "outstanding_minor": outstanding,
        "currency": "INR",
        "status": status,
        "source_updated_at": "2026-08-15T04:20:00Z",
    })
    if err != nil { t.Fatal(err) }
    return Message{ID: "customer-" + id + "-" + name, Type: "customer.upsert", SchemaVersion: 1, Source: "central", Payload: payload}
}

func TestV1CentralCustomerReusesMappedOfflineIdentityAndAppliesCanonicalFacts(t *testing.T) {
    ctx := context.Background()
    db := testutil.OpenDatabase(t)
    repo := customer.NewRepository(db)
    svc := New(db)

    local, err := repo.Create(ctx, customer.UpsertInput{
        Name: "Offline Customer",
        CreditLimitMinor: 990000,
        Currency: "INR",
    })
    if err != nil { t.Fatalf("create offline customer: %v", err) }

    msg := customerUpsertMessage(t, "42", "42", local.ID, 1, "Canonical Customer", "central@example.com", "GST-CENTRAL", 15025, 2050, "inactive")
    if err := svc.Apply(ctx, msg); err != nil { t.Fatalf("apply canonical customer: %v", err) }

    projected, err := repo.Get(ctx, local.ID)
    if err != nil { t.Fatalf("get reconciled customer: %v", err) }
    if projected.ID != local.ID { t.Fatalf("id = %s, want mapped local id %s", projected.ID, local.ID) }
    if projected.Name != "Canonical Customer" { t.Fatalf("name = %s", projected.Name) }
    if projected.Email == nil || *projected.Email != "central@example.com" { t.Fatalf("email = %v", projected.Email) }
    if projected.TaxID == nil || *projected.TaxID != "GST-CENTRAL" { t.Fatalf("tax id = %v", projected.TaxID) }
    if projected.CreditLimitMinor != 15025 || projected.OutstandingMinor != 2050 { t.Fatalf("financial snapshots = %d/%d", projected.CreditLimitMinor, projected.OutstandingMinor) }
    if projected.Status != "inactive" || projected.SyncState != "synced" { t.Fatalf("status/sync = %s/%s", projected.Status, projected.SyncState) }

    var total int
    if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM customers`).Scan(&total); err != nil { t.Fatal(err) }
    if total != 1 { t.Fatalf("customer rows = %d, want exactly one", total) }
    var canonicalDuplicate int
    if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM customers WHERE id='42'`).Scan(&canonicalDuplicate); err != nil { t.Fatal(err) }
    if canonicalDuplicate != 0 { t.Fatalf("canonical duplicate rows = %d", canonicalDuplicate) }
}

func TestV1CentralCustomerDoesNotOverwriteNewerPendingLocalEdit(t *testing.T) {
    ctx := context.Background()
    db := testutil.OpenDatabase(t)
    repo := customer.NewRepository(db)
    svc := New(db)

    local, err := repo.Create(ctx, customer.UpsertInput{Name: "Offline V1", CreditLimitMinor: 100, Currency: "INR"})
    if err != nil { t.Fatal(err) }
    if err := svc.Apply(ctx, customerUpsertMessage(t, "42", "42", local.ID, 1, "Central V1", "v1@example.com", "GST1", 200, 10, "active")); err != nil { t.Fatal(err) }

    edited, err := repo.Update(ctx, local.ID, customer.UpsertInput{Name: "Offline V2 Pending", CreditLimitMinor: 300, Currency: "INR"})
    if err != nil { t.Fatal(err) }
    if edited.LocalVersion != 2 || edited.SyncState != "pending" { t.Fatalf("local edit version/state = %d/%s", edited.LocalVersion, edited.SyncState) }

    if err := svc.Apply(ctx, customerUpsertMessage(t, "42", "42", local.ID, 1, "Stale Central", "stale@example.com", "STALE", 999, 999, "inactive")); err != nil { t.Fatal(err) }
    preserved, err := repo.Get(ctx, local.ID)
    if err != nil { t.Fatal(err) }
    if preserved.Name != "Offline V2 Pending" || preserved.SyncState != "pending" || preserved.CreditLimitMinor != 300 {
        t.Fatalf("newer pending edit was overwritten: %+v", preserved)
    }

    if err := svc.Apply(ctx, customerUpsertMessage(t, "42", "42", local.ID, 2, "Central V2", "v2@example.com", "GST2", 400, 25, "active")); err != nil { t.Fatal(err) }
    confirmed, err := repo.Get(ctx, local.ID)
    if err != nil { t.Fatal(err) }
    if confirmed.Name != "Central V2" || confirmed.SyncState != "synced" || confirmed.CreditLimitMinor != 400 || confirmed.OutstandingMinor != 25 {
        t.Fatalf("confirmed mapping did not reconcile: %+v", confirmed)
    }
}

func TestV1CentralOnlyCustomerUsesStableCanonicalIdentity(t *testing.T) {
    ctx := context.Background()
    db := testutil.OpenDatabase(t)
    repo := customer.NewRepository(db)
    svc := New(db)

    if err := svc.Apply(ctx, customerUpsertMessage(t, "99", "99", "", 0, "Central Only", "central-only@example.com", "GST99", 0, 0, "active")); err != nil { t.Fatal(err) }
    projected, err := repo.Get(ctx, "99")
    if err != nil { t.Fatal(err) }
    if projected.ID != "99" || projected.SyncState != "synced" { t.Fatalf("central-only projection = %+v", projected) }
}
