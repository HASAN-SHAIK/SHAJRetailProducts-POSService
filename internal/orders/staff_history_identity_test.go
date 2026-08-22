package orders

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestHistoricalStaffIdentitySurvivesProfileChangeAndDeactivation(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"staff-history-product", "Staff History Product", "unit", 1, 0, 0, 1, "2026-08-22T00:00:00Z",
	); err != nil {
		t.Fatalf("insert catalog product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"staff-history-price", "staff-history-product", "store-history", "INR", 10000, 1, 100, 1, "2026-08-22T00:00:00Z",
	); err != nil {
		t.Fatalf("insert catalog price: %v", err)
	}

	insertLocalUser := func(id, role string) {
		t.Helper()
		if _, err := db.SQL().ExecContext(ctx, `
			INSERT INTO local_users(
				user_id,tenant_id,role,branch_id,all_branch_access,permissions_json,
				pin_salt,pin_hash,pin_iterations,failed_attempts,grant_id,grant_expires_at,enabled,updated_at
			) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, "tenant-history", role, "store-history", 0, `[]`, []byte("salt"), []byte("hash"), 1, 0,
			"grant-"+id, "2099-01-01T00:00:00Z", 1, "2026-08-22T00:00:00Z",
		); err != nil {
			t.Fatalf("insert local user %s: %v", id, err)
		}
	}
	insertLocalUser("staff-history-creator", "cashier")
	insertLocalUser("staff-history-completer", "cashier")

	service := New(db, catalog.NewRepository(db))
	created, err := service.Create(WithCreatorUserID(ctx, "staff-history-creator"), CreateInput{
		ClientOrderID: "staff-history-order",
		StoreID:       "store-history",
		Currency:      "INR",
		Items: []ItemInput{{
			ProductID:     ExternalID("staff-history-product"),
			QuantityMilli: 1000,
		}},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	completed, err := service.CompleteWith(WithCreatorUserID(ctx, "staff-history-completer"), created.ID)
	if err != nil {
		t.Fatalf("complete order: %v", err)
	}
	if completed.CreatedByUserID == nil || *completed.CreatedByUserID != "staff-history-creator" {
		t.Fatalf("unexpected creator before deactivation: %#v", completed.CreatedByUserID)
	}
	if completed.CompletedByUserID == nil || *completed.CompletedByUserID != "staff-history-completer" {
		t.Fatalf("unexpected completer before deactivation: %#v", completed.CompletedByUserID)
	}

	// Central may later change a staff member's role or deactivate local access.
	// Historical transaction relationships must remain the immutable IDs captured
	// at transaction time; the current local staff projection must not rewrite them.
	if _, err := db.SQL().ExecContext(ctx, `
		UPDATE local_users
		SET role='manager', branch_id='other-store', enabled=0, updated_at='2026-08-22T01:00:00Z'
		WHERE user_id IN ('staff-history-creator','staff-history-completer')`); err != nil {
		t.Fatalf("deactivate staff: %v", err)
	}

	readBack, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("read historical order: %v", err)
	}
	if readBack.CreatedByUserID == nil || *readBack.CreatedByUserID != "staff-history-creator" {
		t.Fatalf("creator relationship changed after staff update: %#v", readBack.CreatedByUserID)
	}
	if readBack.CompletedByUserID == nil || *readBack.CompletedByUserID != "staff-history-completer" {
		t.Fatalf("completer relationship changed after staff update: %#v", readBack.CompletedByUserID)
	}

	var snapshotRaw string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT snapshot_json FROM sales_order_snapshots
		WHERE order_id=? ORDER BY version DESC LIMIT 1`, created.ID).Scan(&snapshotRaw); err != nil {
		t.Fatalf("read completion snapshot: %v", err)
	}
	var snapshot Order
	if err := json.Unmarshal([]byte(snapshotRaw), &snapshot); err != nil {
		t.Fatalf("decode completion snapshot: %v", err)
	}
	if snapshot.CreatedByUserID == nil || *snapshot.CreatedByUserID != "staff-history-creator" {
		t.Fatalf("snapshot creator changed after staff update: %#v", snapshot.CreatedByUserID)
	}
	if snapshot.CompletedByUserID == nil || *snapshot.CompletedByUserID != "staff-history-completer" {
		t.Fatalf("snapshot completer changed after staff update: %#v", snapshot.CompletedByUserID)
	}
}
