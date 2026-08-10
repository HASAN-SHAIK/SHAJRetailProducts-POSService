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

func TestSensitiveApprovalWithoutStoredReasonFailsClosedWithoutConsumption(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	token := "legacy-refund-approval-without-reason"
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(
			token_hash,cashier_user_id,approver_user_id,permission,reason,created_at,expires_at
		) VALUES(?,?,?,?,NULL,?,?)`,
		hash[:], "cashier-1", "manager-1", permissionPOSRefund,
		now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	s := &Server{db: db}
	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err == nil {
		t.Fatal("legacy sensitive approval without reason was accepted")
	}

	var consumedAt sql.NullString
	if err := db.SQL().QueryRowContext(ctx,
		`SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt.Valid {
		t.Fatalf("invalid sensitive approval was consumed at %s", consumedAt.String)
	}

	// Prove the failed validation did not burn the one-time token. Once the
	// legacy row is repaired with an auditable manager reason, the same token
	// can still be consumed exactly once.
	if _, err := db.SQL().ExecContext(ctx,
		`UPDATE pos_manager_approvals SET reason=? WHERE token_hash=?`, "manager approved refund", hash[:]); err != nil {
		t.Fatal(err)
	}
	approval, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund)
	if err != nil {
		t.Fatalf("repaired approval could not be consumed: %v", err)
	}
	if approval.Reason != "manager approved refund" {
		t.Fatalf("approval reason=%q", approval.Reason)
	}
	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err == nil {
		t.Fatal("repaired approval became reusable after consumption")
	}
}

func TestDiscountApprovalWithoutStoredReasonRemainsCompatible(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	token := "legacy-discount-approval-without-reason"
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(
			token_hash,cashier_user_id,approver_user_id,permission,reason,created_at,expires_at
		) VALUES(?,?,?,?,NULL,?,?)`,
		hash[:], "cashier-1", "manager-1", permissionPOSDiscount,
		now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	approval, err := (&Server{db: db}).consumeManagerApproval(ctx, token, "cashier-1", permissionPOSDiscount)
	if err != nil {
		t.Fatalf("discount compatibility regressed: %v", err)
	}
	if approval.Reason != "" {
		t.Fatalf("discount approval reason=%q want empty", approval.Reason)
	}
}
