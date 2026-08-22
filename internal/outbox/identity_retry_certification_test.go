package outbox

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestIdentityPayloadSurvivesLostAckRetryWithoutMutation(t *testing.T) {
	db := testutil.OpenDatabase(t)
	service := New(db)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	payload := map[string]any{
		"order": map[string]any{
			"id":                   "ord-identity-retry-1",
			"store_id":             "store-1",
			"terminal_id":          "terminal-1",
			"customer_id":          "cus-local-1",
			"created_by_user_id":   "staff-creator-1",
			"completed_by_user_id": "staff-cashier-2",
		},
		"actor": map[string]any{
			"user_id":   "staff-cashier-2",
			"tenant_id": "tenant-1",
			"branch_id": "store-1",
			"role":      "cashier",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.SQL().ExecContext(ctx, `INSERT INTO outbox_events(
		id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
		payload_json,metadata_json,status,attempt_count,available_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`,
		"evt-identity-retry-1", "sales_order", "ord-identity-retry-1", 2, "sale.completed", 1,
		"sales_order:ord-identity-retry-1", string(raw), `{"tenant_id":"tenant-1","store_id":"store-1","terminal_id":"terminal-1"}`, now, now)
	if err != nil {
		t.Fatalf("seed identity event: %v", err)
	}

	first, err := service.ClaimNext(ctx, "worker-before-lost-ack")
	if err != nil || first == nil {
		t.Fatalf("first claim event=%v err=%v", first, err)
	}
	firstPayload := append([]byte(nil), first.Payload...)
	firstMetadata := append([]byte(nil), first.Metadata...)

	// Model a publish where Central may have committed but the POS never received
	// the acknowledgement. The POS must retry the same durable event identity and
	// payload instead of generating or inferring replacement identities.
	if err := service.MarkFailed(ctx, first.ID, "worker-before-lost-ack", "acknowledgement lost"); err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET available_at=? WHERE id=?`, past, first.ID); err != nil {
		t.Fatal(err)
	}

	second, err := service.ClaimNext(ctx, "worker-after-restart")
	if err != nil || second == nil {
		t.Fatalf("retry claim event=%v err=%v", second, err)
	}
	if second.ID != first.ID {
		t.Fatalf("retry changed event identity: first=%s second=%s", first.ID, second.ID)
	}
	if second.AggregateID != first.AggregateID || second.AggregateVersion != first.AggregateVersion {
		t.Fatalf("retry changed aggregate identity/version: first=%s/%d second=%s/%d", first.AggregateID, first.AggregateVersion, second.AggregateID, second.AggregateVersion)
	}
	if string(second.Payload) != string(firstPayload) {
		t.Fatalf("retry mutated identity payload\nfirst=%s\nsecond=%s", firstPayload, second.Payload)
	}
	if string(second.Metadata) != string(firstMetadata) {
		t.Fatalf("retry mutated identity metadata\nfirst=%s\nsecond=%s", firstMetadata, second.Metadata)
	}

	var decoded map[string]any
	if err := json.Unmarshal(second.Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	order := decoded["order"].(map[string]any)
	actor := decoded["actor"].(map[string]any)
	if order["customer_id"] != "cus-local-1" || order["created_by_user_id"] != "staff-creator-1" || order["completed_by_user_id"] != "staff-cashier-2" {
		t.Fatalf("retry lost order relationships: %#v", order)
	}
	if actor["user_id"] != "staff-cashier-2" || actor["tenant_id"] != "tenant-1" || actor["branch_id"] != "store-1" {
		t.Fatalf("retry lost actor scope: %#v", actor)
	}
}
