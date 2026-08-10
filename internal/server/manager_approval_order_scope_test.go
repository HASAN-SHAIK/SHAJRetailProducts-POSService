package server

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestVoidApprovalIsBoundToExactOrderWithoutBurningOnMismatch(t *testing.T) {
	testSensitiveApprovalIsBoundToExactOrderWithoutBurningOnMismatch(t, permissionPOSVoid)
}

func TestRefundApprovalIsBoundToExactOrderWithoutBurningOnMismatch(t *testing.T) {
	testSensitiveApprovalIsBoundToExactOrderWithoutBurningOnMismatch(t, permissionPOSRefund)
}

func testSensitiveApprovalIsBoundToExactOrderWithoutBurningOnMismatch(t *testing.T, permission string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }

	s := &Server{db: db}
	token := permission + "-order-scoped-approval"
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(token_hash,cashier_user_id,approver_user_id,permission,reason,order_id,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		hash[:], "cashier-1", "manager-1", permission, "wrong item", "order-1", now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil { t.Fatal(err) }

	if _, err := s.consumeManagerApprovalForOrder(ctx, token, "cashier-1", permission, "order-2"); err == nil {
		t.Fatal("expected approval to reject a different order")
	}

	var consumedAt *string
	if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt != nil {
		t.Fatalf("order mismatch burned approval: consumed_at=%v", *consumedAt)
	}

	approval, err := s.consumeManagerApprovalForOrder(ctx, token, "cashier-1", permission, "order-1")
	if err != nil { t.Fatalf("expected original order to consume approval: %v", err) }
	if approval.ApproverUserID != "manager-1" || approval.Reason != "wrong item" {
		t.Fatalf("unexpected approval: %+v", approval)
	}

	if _, err := s.consumeManagerApprovalForOrder(ctx, token, "cashier-1", permission, "order-1"); err == nil {
		t.Fatal("expected scoped approval replay to fail")
	}
}
