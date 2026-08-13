package server

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestInventoryDeadLetterIsVisibleWithRecoveryDiagnostics(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
			payload_json,metadata_json,status,attempt_count,available_at,last_error,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evt-inventory-dead-letter",
		"inventory_movement",
		"movement-42",
		1,
		"inventory.movement.recorded",
		1,
		"inventory:store-1:product-101",
		`{"movement":{"id":"movement-42","store_id":"store-1","product_id":"101","movement_type":"sale_issue","quantity_delta_milli":-1000}}`,
		`{"source":"pos","order_id":"order-42"}`,
		"dead_letter",
		12,
		"2026-08-13T06:00:00Z",
		"canonical inventory projection rejected",
		"2026-08-13T05:59:00Z",
	)
	if err != nil {
		t.Fatalf("insert dead-letter inventory event: %v", err)
	}

	s := &Server{db: db}
	items, err := s.loadOutboxDiagnostics(ctx, 10)
	if err != nil {
		t.Fatalf("load outbox diagnostics: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one diagnostic item, got %d", len(items))
	}

	item := items[0]
	if item.ID != "evt-inventory-dead-letter" || item.Status != "dead_letter" {
		t.Fatalf("unexpected event identity/status: id=%q status=%q", item.ID, item.Status)
	}
	if item.EventType != "inventory.movement.recorded" || item.AggregateType != "inventory_movement" || item.AggregateID != "movement-42" {
		t.Fatalf("inventory identity missing from diagnostics: %+v", item)
	}
	if item.OrderingKey != "inventory:store-1:product-101" || item.AttemptCount != 12 {
		t.Fatalf("recovery diagnostics missing ordering/attempt evidence: key=%q attempts=%d", item.OrderingKey, item.AttemptCount)
	}
	if item.LastError != "canonical inventory projection rejected" {
		t.Fatalf("recovery diagnostics missing last error: %q", item.LastError)
	}
	if string(item.Payload) == "" || string(item.Metadata) == "" {
		t.Fatal("inventory dead-letter diagnostics must retain payload and metadata for support recovery")
	}
}
