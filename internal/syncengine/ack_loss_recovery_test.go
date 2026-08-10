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

func TestRefundEventRecoversWhenCentralAcceptsButAcknowledgementIsLost(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos-ack-loss.db")
	db := openRealPartialReturnE2EDatabase(t, dbPath)
	defer func() { _ = db.Close() }()

	eventID := "evt-partial-return-ack-loss"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload, err := json.Marshal(map[string]any{
		"return_id": "ret-ack-loss",
		"order": map[string]any{"id": "ord-ack-loss", "status": "completed", "version": 3},
		"refund_minor": 2500,
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(map[string]any{"source": "pos_service", "occurred_at": now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
			ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`,
		eventID, "sales_order", "ord-ack-loss", 3, "sale.partial_returned", 1,
		"sales_order:ord-ack-loss", string(payload), string(metadata), now, now); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	accepted := map[string]bool{}
	requests := 0
	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sync/events" {
			http.NotFound(w, r)
			return
		}
		idempotencyKey := r.Header.Get("Idempotency-Key")
		if idempotencyKey != eventID {
			t.Errorf("idempotency key=%q want=%q", idempotencyKey, eventID)
			http.Error(w, "bad idempotency key", http.StatusBadRequest)
			return
		}

		mu.Lock()
		requests++
		alreadyAccepted := accepted[idempotencyKey]
		if !alreadyAccepted {
			accepted[idempotencyKey] = true
		}
		mu.Unlock()

		if alreadyAccepted {
			// Mirrors the production Central sync contract: a duplicate durable
			// event is acknowledged explicitly with the same event identity.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"code":     "SYNC_EVENT_ALREADY_RECEIVED",
				"event_id": idempotencyKey,
			})
			return
		}

		// Central has accepted/committed the durable event, but the network dies
		// before the POS receives an HTTP acknowledgement.
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
	}))
	defer central.Close()

	engine, err := New(outbox.New(db), central.URL, "tenant-e2e", "sync-secret", "device-e2e", 2*time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected first dispatch attempt")
	}

	var status string
	var attempts int
	if err := db.SQL().QueryRowContext(ctx, `SELECT status,attempt_count FROM outbox_events WHERE id=?`, eventID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || attempts < 1 {
		t.Fatalf("lost acknowledgement must leave retryable durable event status=%s attempts=%d", status, attempts)
	}

	// Simulate POS restart while Central already owns the event but the local
	// outbox still believes delivery failed.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openRealPartialReturnE2EDatabase(t, dbPath)
	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET available_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), eventID); err != nil {
		t.Fatal(err)
	}

	engine, err = New(outbox.New(db), central.URL, "tenant-e2e", "sync-secret", "device-e2e", 2*time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.dispatchOne(ctx) {
		t.Fatal("expected retry dispatch after restart")
	}

	var publishedAt *string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status,published_at FROM outbox_events WHERE id=?`, eventID).Scan(&status, &publishedAt); err != nil {
		t.Fatal(err)
	}
	if status != "published" || publishedAt == nil {
		t.Fatalf("duplicate-safe retry must converge local outbox to published status=%s published_at=%v", status, publishedAt)
	}

	mu.Lock()
	requestCount := requests
	acceptedCount := len(accepted)
	mu.Unlock()
	if requestCount != 2 || acceptedCount != 1 {
		t.Fatalf("ack-loss recovery requests=%d logical central accepts=%d want requests=2 accepts=1", requestCount, acceptedCount)
	}
}
