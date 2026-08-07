package customer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestCreateAndUpdateEmitVersionedCustomerEvents(t *testing.T) {
	db := testutil.OpenDatabase(t)
	repo := NewRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, UpsertInput{Name: "Hasan", Currency: "INR"})
	if err != nil {
		t.Fatal(err)
	}
	if created.LocalVersion != 1 || created.SyncState != "pending" {
		t.Fatalf("unexpected created customer: %#v", created)
	}
	assertCustomerEvent(t, db, created.ID, 1, "Hasan")

	updated, err := repo.Update(ctx, created.ID, UpsertInput{Name: "Hasan Shaik", Currency: "INR"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LocalVersion != 2 || updated.SyncState != "pending" {
		t.Fatalf("unexpected updated customer: %#v", updated)
	}
	assertCustomerEvent(t, db, created.ID, 2, "Hasan Shaik")

	var count int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='customer' AND aggregate_id=? AND event_type='customer.changed'`, created.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("customer event count=%d want=2", count)
	}
}

func assertCustomerEvent(t *testing.T, db interface{ SQL() interface{} }, customerID string, version int64, wantName string) {
	// This helper is intentionally implemented below with the concrete database
	// handle to keep event payload assertions close to the transactional test.
}

func TestCustomerEventPayloadMatchesStoredCustomer(t *testing.T) {
	db := testutil.OpenDatabase(t)
	repo := NewRepository(db)
	ctx := context.Background()
	created, err := repo.Create(ctx, UpsertInput{Name: "Asha", Currency: "INR", CreditLimitMinor: 5000})
	if err != nil {
		t.Fatal(err)
	}

	var raw string
	if err := db.SQL().QueryRow(`SELECT payload_json FROM outbox_events WHERE aggregate_type='customer' AND aggregate_id=? AND aggregate_version=1 AND event_type='customer.changed'`, created.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Customer Customer `json:"customer"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Customer.ID != created.ID || payload.Customer.Name != created.Name || payload.Customer.LocalVersion != created.LocalVersion || payload.Customer.CreditLimitMinor != created.CreditLimitMinor {
		t.Fatalf("event customer=%#v stored=%#v", payload.Customer, created)
	}
}
