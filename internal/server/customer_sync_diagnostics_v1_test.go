package server

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestCustomerSyncFailuresRemainSupportVisible(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
			payload_json,metadata_json,status,attempt_count,available_at,last_error,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evt-customer-dead-letter",
		"customer",
		"cus-offline-42",
		3,
		"customer.changed",
		1,
		"customer:cus-offline-42",
		`{"customer":{"id":"cus-offline-42","name":"Offline Customer","version":3}}`,
		`{"source":"pos_service"}`,
		"dead_letter",
		12,
		"2026-08-15T08:00:00Z",
		"canonical customer projection rejected",
		"2026-08-15T07:59:00Z",
	)
	if err != nil {
		t.Fatalf("insert customer dead-letter event: %v", err)
	}

	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO inbox_messages(
			message_id,message_type,schema_version,source,payload_json,status,attempt_count,received_at,last_error
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"msg-customer-upsert-failed",
		"customer.upsert",
		1,
		"central",
		`{"customer":{"id":"42","name":"Canonical Customer"}}`,
		"failed",
		4,
		"2026-08-15T08:01:00Z",
		"local customer reconciliation rejected",
	)
	if err != nil {
		t.Fatalf("insert failed customer inbox message: %v", err)
	}

	s := &Server{db: db}
	outboxItems, err := s.loadOutboxDiagnostics(ctx, 10)
	if err != nil {
		t.Fatalf("load outbox diagnostics: %v", err)
	}
	if len(outboxItems) != 1 {
		t.Fatalf("expected one customer outbox diagnostic item, got %d", len(outboxItems))
	}
	outbound := outboxItems[0]
	if outbound.ID != "evt-customer-dead-letter" || outbound.EventType != "customer.changed" || outbound.AggregateID != "cus-offline-42" {
		t.Fatalf("customer outbound identity missing from diagnostics: %+v", outbound)
	}
	if outbound.Status != "dead_letter" || outbound.AttemptCount != 12 || outbound.LastError != "canonical customer projection rejected" {
		t.Fatalf("customer outbound failure evidence incomplete: %+v", outbound)
	}
	if string(outbound.Payload) == "" || string(outbound.Metadata) == "" {
		t.Fatal("customer dead-letter diagnostics must retain payload and metadata")
	}

	inboxItems, err := s.loadInboxDiagnostics(ctx, 10)
	if err != nil {
		t.Fatalf("load inbox diagnostics: %v", err)
	}
	if len(inboxItems) != 1 {
		t.Fatalf("expected one customer inbox diagnostic item, got %d", len(inboxItems))
	}
	inbound := inboxItems[0]
	if inbound.MessageID != "msg-customer-upsert-failed" || inbound.MessageType != "customer.upsert" || inbound.Source != "central" {
		t.Fatalf("customer inbound identity missing from diagnostics: %+v", inbound)
	}
	if inbound.Status != "failed" || inbound.AttemptCount != 4 || inbound.LastError != "local customer reconciliation rejected" {
		t.Fatalf("customer inbound failure evidence incomplete: %+v", inbound)
	}
	if string(inbound.Payload) == "" {
		t.Fatal("failed customer inbox diagnostics must retain payload")
	}
}
