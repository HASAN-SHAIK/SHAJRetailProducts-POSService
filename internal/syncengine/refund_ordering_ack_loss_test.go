package syncengine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

func TestRefundOrderingKeyBlocksLaterFactsUntilLostAckEventRecovers(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos-refund-ordering-ack-loss.db")
	db := openRealPartialReturnE2EDatabase(t, dbPath)
	defer func() { _ = db.Close() }()

	orderID := "ord-refund-ordering-ack-loss"
	orderingKey := "sales_order:" + orderID
	now := time.Now().UTC()

	type eventSeed struct {
		id            string
		aggregateType string
		aggregateID   string
		version       int
		eventType     string
	}
	events := []eventSeed{
		{id: "evt-refund-ordering-payment", aggregateType: "payment", aggregateID: "pay-refund-ordering", version: 1, eventType: "payment.recorded"},
		{id: "evt-refund-ordering-inventory", aggregateType: "inventory_movement", aggregateID: "mov-refund-ordering", version: 1, eventType: "inventory.movement.recorded"},
		{id: "evt-refund-ordering-partial", aggregateType: "sales_order", aggregateID: orderID, version: 3, eventType: "sale.partial_returned"},
	}
	for i, event := range events {
		payload, err := json.Marshal(map[string]any{"order_id": orderID, "sequence": i + 1})
		if err != nil {
			t.Fatal(err)
		}
		occurredAt := now.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)
		metadata, err := json.Marshal(map[string]any{"source": "pos_service", "occurred_at": occurredAt})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL().ExecContext(ctx, `
			INSERT INTO outbox_events(
				id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
				ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
			) VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`,
			event.id, event.aggregateType, event.aggregateID, event.version, event.eventType, 1,
			orderingKey, string(payload), string(metadata), occurredAt, occurredAt); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	requests := []string{}
	accepted := map[string]bool{}
	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sync/events" {
			http.NotFound(w, r)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		mu.Lock()
		requests = append(requests, key)
		alreadyAccepted := accepted[key]
		if !alreadyAccepted {
			accepted[key] = true
		}
		requestNumber := len(requests)
		mu.Unlock()

		if key == events[0].id && requestNumber == 1 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("test server does not support connection hijacking")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack acknowledgement connection: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		if alreadyAccepted {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer central.Close()

	engine, err := New(outbox.New(db), central.URL, "tenant-e2e", "sync-secret", "device-e2e", 2*time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected first refund fact dispatch attempt")
	}

	var status string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM outbox_events WHERE id=?`, events[0].id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("lost acknowledgement must leave earliest refund fact retryable, status=%s", status)
	}

	if engine.dispatchOne(ctx) {
		t.Fatal("later refund fact overtook an earlier failed event with the same ordering key")
	}
	mu.Lock()
	if len(requests) != 1 || requests[0] != events[0].id {
		t.Fatalf("refund ordering before recovery requests=%v want=[%s]", requests, events[0].id)
	}
	mu.Unlock()

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openRealPartialReturnE2EDatabase(t, dbPath)
	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET available_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), events[0].id); err != nil {
		t.Fatal(err)
	}
	engine, err = New(outbox.New(db), central.URL, "tenant-e2e", "sync-secret", "device-e2e", 2*time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < len(events); i++ {
		if !engine.dispatchOne(ctx) {
			t.Fatalf("expected ordered refund recovery dispatch %d", i+1)
		}
	}

	for _, event := range events {
		if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM outbox_events WHERE id=?`, event.id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "published" {
			t.Fatalf("refund event %s did not converge to published, status=%s", event.id, status)
		}
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	logicalAccepts := len(accepted)
	mu.Unlock()
	wantRequests := []string{events[0].id, events[0].id, events[1].id, events[2].id}
	if len(gotRequests) != len(wantRequests) {
		t.Fatalf("refund recovery requests=%v want=%v", gotRequests, wantRequests)
	}
	for i := range wantRequests {
		if gotRequests[i] != wantRequests[i] {
			t.Fatalf("refund ordering request[%d]=%s want=%s all=%v", i, gotRequests[i], wantRequests[i], gotRequests)
		}
	}
	if logicalAccepts != 3 {
		t.Fatalf("logical Central accepts=%d want=3", logicalAccepts)
	}
}
