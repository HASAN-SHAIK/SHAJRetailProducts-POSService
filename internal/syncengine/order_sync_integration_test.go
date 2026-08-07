package syncengine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestOutboxSyncRetriesAfterCentralRestoresAndIsIdempotent(t *testing.T) {
	db := testutil.OpenDatabase(t)
	service := outbox.New(db)
	seedSyncEvent(t, db, "evt-retry")

	centralUnavailable := true
	accepted := map[string]int{}
	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if centralUnavailable {
			http.Error(w, "central unavailable", http.StatusServiceUnavailable)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			t.Error("missing idempotency key")
		}
		if accepted[key] > 0 {
			w.WriteHeader(http.StatusConflict)
			return
		}
		var envelope Envelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode envelope: %v", err)
		}
		if envelope.EventID != "evt-retry" || envelope.EventType != "sale.completed" {
			t.Errorf("unexpected envelope: %#v", envelope)
		}
		accepted[key]++
		w.WriteHeader(http.StatusOK)
	}))
	defer central.Close()

	engine, err := New(service, central.URL, "tenant-1", "secret", "dev-1", time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if !engine.dispatchOne(context.Background()) {
		t.Fatal("expected failed dispatch attempt to do work")
	}
	assertOutboxState(t, db, "evt-retry", "failed", 1)

	makeEventDue(t, db, "evt-retry")
	centralUnavailable = false
	if !engine.dispatchOne(context.Background()) {
		t.Fatal("expected retry dispatch to do work")
	}
	assertOutboxState(t, db, "evt-retry", "published", 1)
	if accepted["evt-retry"] != 1 {
		t.Fatalf("central accepted count=%d", accepted["evt-retry"])
	}

	replayPublishedEvent(t, db, "evt-retry")
	if !engine.dispatchOne(context.Background()) {
		t.Fatal("expected idempotent replay dispatch to do work")
	}
	assertOutboxState(t, db, "evt-retry", "published", 1)
	if accepted["evt-retry"] != 1 {
		t.Fatalf("duplicate central insert: %d", accepted["evt-retry"])
	}
}

func TestStaleProcessingEventIsRecoveredAfterRestart(t *testing.T) {
	db := testutil.OpenDatabase(t)
	service := outbox.New(db)
	seedSyncEvent(t, db, "evt-crash")

	event, err := service.ClaimNext(context.Background(), "sync:old-process")
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected claim")
	}
	_, err = db.SQL().Exec(`UPDATE outbox_events SET locked_at=? WHERE id=?`, time.Now().UTC().Add(-5*time.Minute).Format(time.RFC3339Nano), "evt-crash")
	if err != nil {
		t.Fatal(err)
	}

	if err := service.RecoverStaleClaims(context.Background(), 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	assertOutboxState(t, db, "evt-crash", "failed", 0)
	makeEventDue(t, db, "evt-crash")

	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "evt-crash" {
			t.Errorf("idempotency=%s", r.Header.Get("Idempotency-Key"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer central.Close()

	engine, err := New(service, central.URL, "tenant-1", "secret", "dev-1", time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(context.Background()) {
		t.Fatal("expected recovered event to dispatch")
	}
	assertOutboxState(t, db, "evt-crash", "published", 0)
}

func seedSyncEvent(t *testing.T, db *database.DB, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.SQL().Exec(`INSERT INTO outbox_events(
        id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
        payload_json,metadata_json,status,attempt_count,available_at,created_at)
        VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`,
		id, "sales_order", "order-1", 2, "sale.completed", 1, "sales_order:order-1",
		`{"order":{"id":"order-1"}}`, `{"source":"pos_service"}`, now, now)
	if err != nil {
		t.Fatalf("seed sync event: %v", err)
	}
}

func makeEventDue(t *testing.T, db *database.DB, id string) {
	t.Helper()
	due := time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	if _, err := db.SQL().Exec(`UPDATE outbox_events SET available_at=? WHERE id=?`, due, id); err != nil {
		t.Fatalf("make event due: %v", err)
	}
}

func replayPublishedEvent(t *testing.T, db *database.DB, id string) {
	t.Helper()
	due := time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	_, err := db.SQL().Exec(`UPDATE outbox_events SET status='pending', published_at=NULL, available_at=? WHERE id=?`, due, id)
	if err != nil {
		t.Fatalf("replay event: %v", err)
	}
}

func assertOutboxState(t *testing.T, db *database.DB, id, wantStatus string, wantAttempts int) {
	t.Helper()
	var status string
	var attempts int
	if err := db.SQL().QueryRow(`SELECT status,attempt_count FROM outbox_events WHERE id=?`, id).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || attempts != wantAttempts {
		t.Fatalf("event %s status=%s attempts=%d, want status=%s attempts=%d", id, status, attempts, wantStatus, wantAttempts)
	}
}
