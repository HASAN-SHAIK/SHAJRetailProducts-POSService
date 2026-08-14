package server

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1InventoryOperatorVisibilityCombinesLocalTruthAndSyncState(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at)
		VALUES('store-1','product-101',3500,500,7,'2026-08-14T03:30:00Z')`)
	if err != nil {
		t.Fatalf("insert balance: %v", err)
	}

	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO inventory_movements(
			id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,
			order_item_id,balance_after_milli,occurred_at,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"movement-visibility-1", "store-1", "product-101", "sale_issue", -1000,
		"sale_order", "order-visibility-1", "item-visibility-1", 3500,
		"2026-08-14T03:30:00Z", "2026-08-14T03:30:00Z",
	)
	if err != nil {
		t.Fatalf("insert movement: %v", err)
	}

	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
			payload_json,metadata_json,status,attempt_count,available_at,last_error,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evt-inventory-visibility-1",
		"inventory_movement",
		"movement-visibility-1",
		1,
		"inventory.movement.recorded",
		1,
		"inventory:store-1:product-101",
		`{"movement":{"id":"movement-visibility-1","store_id":"store-1","product_id":"product-101","movement_type":"sale_issue","quantity_delta_milli":-1000,"balance_after_milli":3500}}`,
		`{"source":"pos","order_id":"order-visibility-1"}`,
		"failed",
		3,
		"2026-08-14T03:31:00Z",
		"central temporarily unavailable",
		"2026-08-14T03:30:00Z",
	)
	if err != nil {
		t.Fatalf("insert outbox event: %v", err)
	}

	inventoryService := inventory.New(db)
	balance, err := inventoryService.GetBalance(ctx, "store-1", "product-101")
	if err != nil {
		t.Fatalf("load local inventory balance: %v", err)
	}
	if balance.OnHandMilli != 3500 || balance.ReservedMilli != 500 || balance.AvailableMilli != 3000 {
		t.Fatalf("unexpected local inventory truth: %+v", balance)
	}

	movements, err := inventoryService.ListMovements(ctx, "store-1", "product-101", 10)
	if err != nil {
		t.Fatalf("load inventory movements: %v", err)
	}
	if len(movements) != 1 || movements[0].ID != "movement-visibility-1" || movements[0].BalanceAfterMilli != 3500 {
		t.Fatalf("movement history does not expose immutable local inventory fact: %+v", movements)
	}

	s := &Server{db: db}
	events, err := s.loadOutboxDiagnostics(ctx, 10)
	if err != nil {
		t.Fatalf("load sync diagnostics: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one pending/blocked inventory sync fact, got %d", len(events))
	}
	event := events[0]
	if event.AggregateID != "movement-visibility-1" || event.EventType != "inventory.movement.recorded" {
		t.Fatalf("sync diagnostic cannot be correlated to inventory movement: %+v", event)
	}
	if event.Status != "failed" || event.AttemptCount != 3 || event.LastError != "central temporarily unavailable" {
		t.Fatalf("operator cannot distinguish blocked inventory convergence: %+v", event)
	}
}
