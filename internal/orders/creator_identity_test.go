package orders

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestCreatePersistsAuthenticatedCreatorInInitialOrderAndSnapshot(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"creator-product", "Creator Product", "unit", 1, 0, 0, 1, "2026-08-21T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog product: %v", err)
	}
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"creator-price", "creator-product", "store-1", "INR", 12500, 1, 100, 1, "2026-08-21T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog price: %v", err)
	}

	service := New(db, catalog.NewRepository(db))
	creatorCtx := WithCreatorUserID(ctx, "staff-central-42")
	created, err := service.Create(creatorCtx, CreateInput{
		ClientOrderID: "creator-order-1",
		StoreID:       "store-1",
		Currency:      "INR",
		Items: []ItemInput{{
			ProductID:     ExternalID("creator-product"),
			QuantityMilli: 1000,
		}},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if created.CreatedByUserID == nil || *created.CreatedByUserID != "staff-central-42" {
		t.Fatalf("expected returned creator staff-central-42, got %#v", created.CreatedByUserID)
	}

	var persistedCreator string
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT created_by_user_id FROM sales_orders WHERE id=?`, created.ID,
	).Scan(&persistedCreator); err != nil {
		t.Fatalf("read persisted creator: %v", err)
	}
	if persistedCreator != "staff-central-42" {
		t.Fatalf("expected persisted creator staff-central-42, got %q", persistedCreator)
	}

	var snapshotRaw string
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT snapshot_json FROM sales_order_snapshots WHERE order_id=? AND version=1`, created.ID,
	).Scan(&snapshotRaw); err != nil {
		t.Fatalf("read version-1 snapshot: %v", err)
	}
	var snapshot Order
	if err := json.Unmarshal([]byte(snapshotRaw), &snapshot); err != nil {
		t.Fatalf("decode version-1 snapshot: %v", err)
	}
	if snapshot.CreatedByUserID == nil || *snapshot.CreatedByUserID != "staff-central-42" {
		t.Fatalf("expected snapshot creator staff-central-42, got %#v", snapshot.CreatedByUserID)
	}

	readBack, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if readBack.CreatedByUserID == nil || *readBack.CreatedByUserID != "staff-central-42" {
		t.Fatalf("expected read model creator staff-central-42, got %#v", readBack.CreatedByUserID)
	}

	retried, err := service.Create(WithCreatorUserID(ctx, "staff-central-99"), CreateInput{
		ClientOrderID: "creator-order-1",
		StoreID:       "store-1",
		Currency:      "INR",
		Items: []ItemInput{{
			ProductID:     ExternalID("creator-product"),
			QuantityMilli: 1000,
		}},
	})
	if err != nil {
		t.Fatalf("retry create: %v", err)
	}
	if retried.ID != created.ID {
		t.Fatalf("expected idempotent retry to return %s, got %s", created.ID, retried.ID)
	}
	if retried.CreatedByUserID == nil || *retried.CreatedByUserID != "staff-central-42" {
		t.Fatalf("idempotent retry must preserve original creator, got %#v", retried.CreatedByUserID)
	}
}
