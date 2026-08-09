package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func openManagerApprovalRestartDB(t *testing.T, path string) *database.DB {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestUnconsumedManagerApprovalSurvivesPOSRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")
	db := openManagerApprovalRestartDB(t, dbPath)

	const token = "restart-unconsumed-refund-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSRefund)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db = openManagerApprovalRestartDB(t, dbPath)
	defer db.Close()
	s := &Server{db: db}

	approval, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund)
	if err != nil {
		t.Fatalf("valid unconsumed approval did not survive restart: %v", err)
	}
	if approval.ApproverUserID != "manager-1" || approval.Permission != permissionPOSRefund {
		t.Fatalf("approval after restart=%+v", approval)
	}
}

func TestConsumedManagerApprovalRemainsConsumedAfterPOSRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")
	db := openManagerApprovalRestartDB(t, dbPath)

	const token = "restart-consumed-void-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSVoid)
	s := &Server{db: db}
	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSVoid); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db = openManagerApprovalRestartDB(t, dbPath)
	defer db.Close()
	s = &Server{db: db}
	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSVoid); err == nil {
		t.Fatal("consumed approval became reusable after restart")
	}
}
