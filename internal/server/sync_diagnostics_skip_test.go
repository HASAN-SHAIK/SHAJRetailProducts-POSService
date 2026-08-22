package server

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestSkipOutboxDiagnosticMarksEventPublishedWithAuditNote(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
			payload_json,metadata_json,status,attempt_count,available_at,last_error,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evt-skip-outbox",
		"customer",
		"cus-skip",
		1,
		"customer.changed",
		1,
		"customer:cus-skip",
		`{"customer":{"id":"cus-skip"}}`,
		`{"source":"pos_service"}`,
		"dead_letter",
		12,
		"2026-08-20T08:00:00Z",
		"canonical customer rejected",
		"2026-08-20T07:59:00Z",
	); err != nil {
		t.Fatalf("seed outbox event: %v", err)
	}

	s := &Server{db: db}
	result, err := s.skipOutboxDiagnostic(ctx, "evt-skip-outbox", "admin-1", "duplicate customer no longer needed")
	if err != nil {
		t.Fatalf("skip outbox event: %v", err)
	}
	if result["status"] != "published" || result["skipped"] != true {
		t.Fatalf("unexpected skip result: %+v", result)
	}

	var status, lastError, metadata string
	var lockedAt sql.NullString
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT status,last_error,metadata_json,locked_at
		FROM outbox_events WHERE id='evt-skip-outbox'`).Scan(&status, &lastError, &metadata, &lockedAt); err != nil {
		t.Fatalf("read skipped outbox event: %v", err)
	}
	if status != "published" || lockedAt.Valid {
		t.Fatalf("outbox event not removed from retryable queue: status=%q locked=%v", status, lockedAt.Valid)
	}
	if !strings.Contains(lastError, "skipped_by_user:admin-1") || !strings.Contains(metadata, "duplicate customer no longer needed") {
		t.Fatalf("skip audit note missing: last_error=%q metadata=%s", lastError, metadata)
	}
}

func TestSkipInboxDiagnosticMarksMessageAppliedWithAuditNote(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inbox_messages(
			message_id,message_type,schema_version,source,payload_json,status,attempt_count,received_at,last_error
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"msg-skip-inbox",
		"catalog.product.upsert",
		1,
		"central",
		`{"id":"p-1","name":"Old Product"}`,
		"failed",
		5,
		"2026-08-20T09:00:00Z",
		"invalid old payload",
	); err != nil {
		t.Fatalf("seed inbox message: %v", err)
	}

	s := &Server{db: db}
	result, err := s.skipInboxDiagnostic(ctx, "msg-skip-inbox", "manager-1", "obsolete snapshot")
	if err != nil {
		t.Fatalf("skip inbox message: %v", err)
	}
	if result["status"] != "applied" || result["skipped"] != true {
		t.Fatalf("unexpected skip result: %+v", result)
	}

	var status, lastError string
	var appliedAt sql.NullString
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT status,last_error,applied_at
		FROM inbox_messages WHERE message_id='msg-skip-inbox'`).Scan(&status, &lastError, &appliedAt); err != nil {
		t.Fatalf("read skipped inbox message: %v", err)
	}
	if status != "applied" || !appliedAt.Valid {
		t.Fatalf("inbox message not removed from retryable queue: status=%q applied=%v", status, appliedAt.Valid)
	}
	if !strings.Contains(lastError, "skipped_by_user:manager-1") || !strings.Contains(lastError, "obsolete snapshot") {
		t.Fatalf("skip audit note missing: %q", lastError)
	}
}
