package customer

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1OfflineCustomerSearchCreateUpdateLifecycle(t *testing.T) {
	db := testutil.OpenDatabase(t)
	repo := NewRepository(db)
	ctx := context.Background()

	phone := "9000012345"
	email := "offline.customer@example.test"
	code := "CUST-OFFLINE-001"
	taxID := "GST-OFFLINE-001"
	created, err := repo.Create(ctx, UpsertInput{
		CustomerCode:     &code,
		Name:             "Offline Customer",
		Phone:            &phone,
		Email:            &email,
		TaxID:            &taxID,
		CreditLimitMinor: 250000,
		Currency:         "inr",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.LocalVersion != 1 || created.SyncState != "pending" || created.Status != "active" || created.Currency != "INR" {
		t.Fatalf("unexpected created customer: %#v", created)
	}

	for _, query := range []string{"offline", "9000012345", "example.test", "cust-offline-001"} {
		matches, err := repo.Search(ctx, query, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 || matches[0].ID != created.ID {
			t.Fatalf("search %q returned %#v", query, matches)
		}
	}

	updatedEmail := "updated.customer@example.test"
	updated, err := repo.Update(ctx, created.ID, UpsertInput{
		CustomerCode:     &code,
		Name:             "Offline Customer Updated",
		Phone:            &phone,
		Email:            &updatedEmail,
		TaxID:            &taxID,
		CreditLimitMinor: 300000,
		Currency:         "INR",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LocalVersion != 2 || updated.SyncState != "pending" || updated.Name != "Offline Customer Updated" || updated.CreditLimitMinor != 300000 {
		t.Fatalf("unexpected updated customer: %#v", updated)
	}

	var eventCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='customer' AND aggregate_id=? AND event_type='customer.changed'`, created.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 {
		t.Fatalf("customer event count=%d want=2", eventCount)
	}

	if _, err := db.SQL().Exec(`UPDATE customers SET status='inactive' WHERE id=?`, created.ID); err != nil {
		t.Fatal(err)
	}
	matches, err := repo.Search(ctx, "Offline Customer Updated", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("inactive customer remained searchable: %#v", matches)
	}

	stored, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "inactive" {
		t.Fatalf("inactive customer not durably retained: %#v", stored)
	}
}

func TestV1OfflineCustomerValidationFailsClosed(t *testing.T) {
	db := testutil.OpenDatabase(t)
	repo := NewRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, UpsertInput{Name: "   ", Currency: "INR"}); err == nil {
		t.Fatal("expected blank customer name to be rejected")
	}
	if _, err := repo.Create(ctx, UpsertInput{Name: "Invalid Credit", CreditLimitMinor: -1, Currency: "INR"}); err == nil {
		t.Fatal("expected negative credit limit to be rejected")
	}
	if _, err := repo.Create(ctx, UpsertInput{Name: "Invalid Currency", Currency: "RUPEES"}); err == nil {
		t.Fatal("expected invalid currency to be rejected")
	}

	var count int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM customers`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid customer mutations persisted %d rows", count)
	}
}
