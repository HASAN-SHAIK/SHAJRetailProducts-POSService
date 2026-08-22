package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestSaleCompletedOutboxCarriesCanonicalCreatorAndCompleterIdentities(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"identity-product", "Identity Product", "unit", 1, 0, 0, 1, "2026-08-21T12:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog product: %v", err)
	}
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"identity-price", "identity-product", "store-1", "INR", 10000, 1, 100, 1, "2026-08-21T12:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog price: %v", err)
	}

	orderService := orders.New(db, catalog.NewRepository(db))
	created, err := orderService.Create(orders.WithCreatorUserID(ctx, "staff-creator-1"), orders.CreateInput{
		ClientOrderID: "identity-outbox-order-1",
		StoreID:       "store-1",
		TerminalID:    stringPointer("terminal-1"),
		Currency:      "INR",
		Items: []orders.ItemInput{{
			ProductID:     orders.ExternalID("identity-product"),
			QuantityMilli: 1000,
		}},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	cashier := LocalUserContext{
		UserID:   "staff-cashier-2",
		Role:     "cashier",
		TenantID: "tenant-1",
		BranchID: "store-1",
	}
	completionCtx := context.WithValue(ctx, authContextKey{}, cashier)
	completionCtx = orders.WithCreatorUserID(completionCtx, cashier.UserID)

	seedSaleCompleted := func(hookCtx context.Context, tx *sql.Tx, order orders.Order) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		payload, err := json.Marshal(map[string]any{
			"order": map[string]any{
				"id":         order.ID,
				"store_id":   order.StoreID,
				"terminal_id": order.TerminalID,
			},
		})
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(hookCtx, `
			INSERT INTO outbox_events(
				id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
				ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
			) VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`,
			"evt-identity-contract-1", "sales_order", order.ID, order.Version, "sale.completed", 1,
			"sales_order:"+order.ID, string(payload), `{}`, now, now,
		)
		return err
	}

	srv := &Server{db: db}
	completed, err := orderService.CompleteWith(
		completionCtx,
		created.ID,
		seedSaleCompleted,
		srv.cashierCompletionAuditHook(completionCtx),
	)
	if err != nil {
		t.Fatalf("complete order: %v", err)
	}
	if completed.CreatedByUserID == nil || *completed.CreatedByUserID != "staff-creator-1" {
		t.Fatalf("creator identity changed during completion: %#v", completed.CreatedByUserID)
	}
	if completed.CompletedByUserID == nil || *completed.CompletedByUserID != cashier.UserID {
		t.Fatalf("expected completer %q, got %#v", cashier.UserID, completed.CompletedByUserID)
	}

	var payloadRaw, metadataRaw string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT payload_json,metadata_json
		FROM outbox_events
		WHERE aggregate_type='sales_order' AND aggregate_id=? AND aggregate_version=? AND event_type='sale.completed'`,
		created.ID, completed.Version,
	).Scan(&payloadRaw, &metadataRaw); err != nil {
		t.Fatalf("read sale.completed outbox event: %v", err)
	}

	var payload struct {
		Actor struct {
			UserID   string  `json:"user_id"`
			Role     string  `json:"role"`
			TenantID string  `json:"tenant_id"`
			BranchID *string `json:"branch_id"`
		} `json:"actor"`
		Order struct {
			CreatedByUserID   *string `json:"created_by_user_id"`
			CompletedByUserID *string `json:"completed_by_user_id"`
		} `json:"order"`
	}
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		t.Fatalf("decode sale.completed payload: %v", err)
	}
	if payload.Order.CreatedByUserID == nil || *payload.Order.CreatedByUserID != "staff-creator-1" {
		t.Fatalf("sale.completed creator = %#v", payload.Order.CreatedByUserID)
	}
	if payload.Order.CompletedByUserID == nil || *payload.Order.CompletedByUserID != cashier.UserID {
		t.Fatalf("sale.completed completer = %#v", payload.Order.CompletedByUserID)
	}
	if payload.Actor.UserID != cashier.UserID || payload.Actor.Role != cashier.Role || payload.Actor.TenantID != cashier.TenantID {
		t.Fatalf("sale.completed actor mismatch: %#v", payload.Actor)
	}
	if payload.Actor.BranchID == nil || *payload.Actor.BranchID != cashier.BranchID {
		t.Fatalf("sale.completed branch actor mismatch: %#v", payload.Actor.BranchID)
	}

	var metadata struct {
		ActorUserID string `json:"actor_user_id"`
		ActorRole   string `json:"actor_role"`
	}
	if err := json.Unmarshal([]byte(metadataRaw), &metadata); err != nil {
		t.Fatalf("decode sale.completed metadata: %v", err)
	}
	if metadata.ActorUserID != cashier.UserID || metadata.ActorRole != cashier.Role {
		t.Fatalf("sale.completed metadata actor mismatch: %#v", metadata)
	}
}

func stringPointer(value string) *string { return &value }
