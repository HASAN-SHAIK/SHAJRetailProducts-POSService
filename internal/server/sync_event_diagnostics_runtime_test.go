package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1LiveSyncDiagnosticsPrioritizesFailuresAndHidesCompletedEvents(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenDatabase(t)
	now := time.Now().UTC()

	insertOutbox := func(id, status string, createdAt time.Time) {
		t.Helper()
		publishedAt := any(nil)
		if status == "published" {
			publishedAt = now.Format(time.RFC3339Nano)
		}
		if _, err := db.SQL().ExecContext(ctx, `
			INSERT INTO outbox_events (
				id, aggregate_type, aggregate_id, aggregate_version, event_type, schema_version,
				ordering_key, payload_json, metadata_json, status, attempt_count, available_at,
				created_at, published_at
			) VALUES (?, 'sale', ?, 1, 'sale.completed', 1, ?, '{}', '{}', ?, 1, ?, ?, ?)`,
			id, id, id, status, createdAt.Format(time.RFC3339Nano), createdAt.Format(time.RFC3339Nano), publishedAt,
		); err != nil {
			t.Fatalf("insert outbox %s: %v", id, err)
		}
	}
	insertInbox := func(id, status string, receivedAt time.Time) {
		t.Helper()
		appliedAt := any(nil)
		if status == "applied" {
			appliedAt = now.Format(time.RFC3339Nano)
		}
		if _, err := db.SQL().ExecContext(ctx, `
			INSERT INTO inbox_messages (
				message_id, message_type, schema_version, source, payload_json, status,
				attempt_count, received_at, applied_at
			) VALUES (?, 'catalog.product.upsert', 1, 'central', '{}', ?, 1, ?, ?)`,
			id, status, receivedAt.Format(time.RFC3339Nano), appliedAt,
		); err != nil {
			t.Fatalf("insert inbox %s: %v", id, err)
		}
	}

	insertOutbox("outbox-pending", "pending", now.Add(-20*time.Minute))
	insertOutbox("outbox-failed", "failed", now.Add(-10*time.Minute))
	insertOutbox("outbox-published", "published", now.Add(-30*time.Minute))
	insertInbox("inbox-received", "received", now.Add(-20*time.Minute))
	insertInbox("inbox-failed", "failed", now.Add(-10*time.Minute))
	insertInbox("inbox-applied", "applied", now.Add(-30*time.Minute))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	catalogRepo := catalog.NewRepository(db)
	app := New(
		config.Config{Environment: "test", ListenAddress: addr},
		db,
		device.New(db),
		catalogRepo,
		customer.NewRepository(db),
		orders.New(db, catalogRepo),
		payments.New(db),
		inventory.New(db),
		receipts.New(db),
	)

	serverErr := make(chan error, 1)
	go func() { serverErr <- app.Start() }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := app.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown POSService: %v", err)
		}
		select {
		case err := <-serverErr:
			if err != nil {
				t.Errorf("POSService stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("POSService did not stop after shutdown")
		}
	})

	client := &http.Client{Timeout: 2 * time.Second}
	baseURL := "http://" + addr
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get(baseURL + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("POSService did not become healthy at %s: %v", baseURL, err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	resp, err := client.Get(baseURL + "/api/v1/diagnostics/sync-events?limit=10")
	if err != nil {
		t.Fatalf("sync diagnostics request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync diagnostics status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	var got syncEventDiagnostics
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode sync diagnostics: %v", err)
	}
	if got.Limit != 10 {
		t.Fatalf("diagnostics limit=%d want=10", got.Limit)
	}
	if len(got.Outbox) != 2 {
		t.Fatalf("outbox diagnostics len=%d want=2 items=%+v", len(got.Outbox), got.Outbox)
	}
	if got.Outbox[0].ID != "outbox-failed" || got.Outbox[0].Status != "failed" {
		t.Fatalf("first outbox diagnostic=%+v want failed event first", got.Outbox[0])
	}
	if got.Outbox[1].ID != "outbox-pending" || got.Outbox[1].Status != "pending" {
		t.Fatalf("second outbox diagnostic=%+v want pending event second", got.Outbox[1])
	}
	if got.Outbox[0].StuckSince == "" || got.Outbox[0].AgeSeconds < 9*60 {
		t.Fatalf("failed outbox missing stuck evidence: %+v", got.Outbox[0])
	}
	if len(got.Inbox) != 2 {
		t.Fatalf("inbox diagnostics len=%d want=2 items=%+v", len(got.Inbox), got.Inbox)
	}
	if got.Inbox[0].MessageID != "inbox-failed" || got.Inbox[0].Status != "failed" {
		t.Fatalf("first inbox diagnostic=%+v want failed message first", got.Inbox[0])
	}
	if got.Inbox[1].MessageID != "inbox-received" || got.Inbox[1].Status != "received" {
		t.Fatalf("second inbox diagnostic=%+v want received message second", got.Inbox[1])
	}
	if got.Inbox[0].StuckSince == "" || got.Inbox[0].AgeSeconds < 9*60 {
		t.Fatalf("failed inbox missing stuck evidence: %+v", got.Inbox[0])
	}

	resp, err = client.Get(baseURL + "/api/v1/diagnostics/sync-events?limit=1")
	if err != nil {
		t.Fatalf("limited sync diagnostics request: %v", err)
	}
	defer resp.Body.Close()
	var limited syncEventDiagnostics
	if err := json.NewDecoder(resp.Body).Decode(&limited); err != nil {
		t.Fatalf("decode limited sync diagnostics: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("limited diagnostics status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	if len(limited.Outbox) != 1 || limited.Outbox[0].ID != "outbox-failed" {
		t.Fatalf("limited outbox=%+v want only failed event", limited.Outbox)
	}
	if len(limited.Inbox) != 1 || limited.Inbox[0].MessageID != "inbox-failed" {
		t.Fatalf("limited inbox=%+v want only failed message", limited.Inbox)
	}

	var publishedCount, appliedCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE status='published'`).Scan(&publishedCount); err != nil {
		t.Fatalf("count published outbox after diagnostics: %v", err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inbox_messages WHERE status='applied'`).Scan(&appliedCount); err != nil {
		t.Fatalf("count applied inbox after diagnostics: %v", err)
	}
	if publishedCount != 1 || appliedCount != 1 {
		t.Fatalf("diagnostics mutated completed sync state published=%d applied=%d", publishedCount, appliedCount)
	}
}