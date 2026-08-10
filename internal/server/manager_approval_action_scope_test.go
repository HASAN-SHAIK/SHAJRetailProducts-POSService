package server

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestRefundApprovalIsBoundToExactActionWithoutBurningOnMismatch(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }

	s := &Server{db: db}
	token := "refund-action-scoped-approval"
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(token_hash,cashier_user_id,approver_user_id,permission,reason,order_id,action_scope,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		hash[:], "cashier-1", "manager-1", permissionPOSRefund, "customer return", "order-1", approvalActionRefundPartial, now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil { t.Fatal(err) }

	if _, err := s.consumeManagerApprovalForRefundAction(ctx, token, "cashier-1", "order-1", approvalActionRefundFull); err == nil {
		t.Fatal("expected partial-refund approval to reject full-refund action")
	}

	var consumedAt *string
	if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt != nil {
		t.Fatalf("action mismatch burned approval: consumed_at=%v", *consumedAt)
	}

	approval, err := s.consumeManagerApprovalForRefundAction(ctx, token, "cashier-1", "order-1", approvalActionRefundPartial)
	if err != nil { t.Fatalf("expected matching partial-refund action to consume approval: %v", err) }
	if approval.ApproverUserID != "manager-1" || approval.Reason != "customer return" {
		t.Fatalf("unexpected approval: %+v", approval)
	}

	if _, err := s.consumeManagerApprovalForRefundAction(ctx, token, "cashier-1", "order-1", approvalActionRefundPartial); err == nil {
		t.Fatal("expected action-scoped approval replay to fail")
	}
}

func TestRefundApprovalActionScopeValidation(t *testing.T) {
	if !validApprovalActionScope(permissionPOSRefund, approvalActionRefundFull) {
		t.Fatal("full refund action should be valid")
	}
	if !validApprovalActionScope(permissionPOSRefund, approvalActionRefundPartial) {
		t.Fatal("partial refund action should be valid")
	}
	if validApprovalActionScope(permissionPOSRefund, "") || validApprovalActionScope(permissionPOSRefund, "refund_other") {
		t.Fatal("refund approval must have an explicit known action scope")
	}
	if validApprovalActionScope(permissionPOSVoid, approvalActionRefundFull) {
		t.Fatal("non-refund approvals must not accept refund action scope")
	}
}
