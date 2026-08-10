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

func TestLegacySensitiveApprovalWithoutReasonRemainsUnconsumedAcrossRestart(t *testing.T) {
	for _, permission := range []string{permissionPOSVoid, permissionPOSRefund} {
		t.Run(permission, func(t *testing.T) {
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

			token := "legacy-sensitive-approval-without-reason-" + permission
			hash := sha256.Sum256([]byte(token))
			now := time.Now().UTC()
			if _, err := db.SQL().ExecContext(ctx, `
				INSERT INTO pos_manager_approvals(
					token_hash,cashier_user_id,approver_user_id,permission,reason,created_at,expires_at
				) VALUES(?,?,?,?,NULL,?,?)`,
				hash[:], "cashier-1", "manager-1", permission,
				now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
				db.Close()
				t.Fatal(err)
			}

			if _, err := (&Server{db: db}).consumeManagerApproval(ctx, token, "cashier-1", permission); err == nil {
				db.Close()
				t.Fatal("legacy sensitive approval without reason was accepted before restart")
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

			if _, err := (&Server{db: db}).consumeManagerApproval(ctx, token, "cashier-1", permission); err == nil {
				t.Fatal("legacy sensitive approval without reason was accepted after restart")
			}

			var consumedAt sql.NullString
			if err := db.SQL().QueryRowContext(ctx,
				`SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
				t.Fatal(err)
			}
			if consumedAt.Valid {
				t.Fatalf("invalid sensitive approval was consumed across restart at %s", consumedAt.String)
			}

			if _, err := db.SQL().ExecContext(ctx,
				`UPDATE pos_manager_approvals SET reason=? WHERE token_hash=?`, "manager approved sensitive action", hash[:]); err != nil {
				t.Fatal(err)
			}
			approval, err := (&Server{db: db}).consumeManagerApproval(ctx, token, "cashier-1", permission)
			if err != nil {
				t.Fatalf("repaired approval could not be consumed after restart: %v", err)
			}
			if approval.Reason != "manager approved sensitive action" {
				t.Fatalf("approval reason=%q", approval.Reason)
			}
			if _, err := (&Server{db: db}).consumeManagerApproval(ctx, token, "cashier-1", permission); err == nil {
				t.Fatal("repaired approval became reusable after consumption")
			}
		})
	}
}
