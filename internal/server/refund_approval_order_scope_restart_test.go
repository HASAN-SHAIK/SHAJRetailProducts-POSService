package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestRefundApprovalWrongOrderRemainsUnconsumedAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")
	db, err := database.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate: %v", err)
	}

	const (
		token       = "refund-order-scoped-approval-restart"
		cashierID   = "cashier-1"
		approvedID  = "order-approved"
		attemptedID = "order-wrong"
	)
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(
			token_hash,cashier_user_id,approver_user_id,permission,reason,order_id,action_scope,created_at,expires_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		hash[:], cashierID, "manager-1", permissionPOSRefund, "approved refund", approvedID, approvalActionRefundFull,
		now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		db.Close()
		t.Fatalf("seed approval: %v", err)
	}

	consume := func(current *database.DB, orderID string) (managerApproval, error) {
		return (&Server{db: current}).consumeManagerApprovalForRefundAction(ctx, token, cashierID, orderID, approvalActionRefundFull)
	}
	if _, err := consume(db, attemptedID); err == nil {
		db.Close()
		t.Fatal("wrong-order refund approval was accepted before restart")
	}

	var consumedAt sql.NullString
	if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if consumedAt.Valid {
		db.Close()
		t.Fatalf("wrong-order attempt burned approval before restart at %s", consumedAt.String)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before restart: %v", err)
	}

	db, err = database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate after restart: %v", err)
	}

	if _, err := consume(db, attemptedID); err == nil {
		t.Fatal("wrong-order refund approval was accepted after restart")
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt.Valid {
		t.Fatalf("wrong-order attempt burned approval across restart at %s", consumedAt.String)
	}

	approval, err := consume(db, approvedID)
	if err != nil {
		t.Fatalf("correct order/action could not consume preserved approval after restart: %v", err)
	}
	if approval.ApproverUserID != "manager-1" || approval.Reason != "approved refund" {
		t.Fatalf("unexpected approval after restart: %+v", approval)
	}
	if _, err := consume(db, approvedID); err == nil {
		t.Fatal("correct-order/action approval remained reusable after consumption")
	}
}
