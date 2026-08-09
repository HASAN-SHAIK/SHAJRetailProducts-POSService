package server

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func seedSensitiveApproval(t *testing.T, db *database.DB, token, cashierUserID, permission string) {
	t.Helper()
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	_, err := db.SQL().ExecContext(context.Background(), `
		INSERT INTO pos_manager_approvals(token_hash,cashier_user_id,approver_user_id,permission,reason,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?)`,
		hash[:], cashierUserID, "manager-1", permission, "approved sensitive action",
		now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}

func openSensitiveApprovalDB(t *testing.T) *database.DB {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRefundApprovalCannotAuthorizeVoidAndIsNotConsumed(t *testing.T) {
	ctx := context.Background()
	db := openSensitiveApprovalDB(t)
	token := "refund-only-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSRefund)

	s := &Server{db: db}
	cashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/void", strings.NewReader(`{"reason":"customer changed mind"}`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleOrderVoid(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "manager_approval_required") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err != nil {
		t.Fatalf("void scope mismatch consumed refund approval: %v", err)
	}
}

func TestVoidApprovalCannotAuthorizeRefundAndIsNotConsumed(t *testing.T) {
	ctx := context.Background()
	db := openSensitiveApprovalDB(t)
	token := "void-only-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSVoid)

	s := &Server{db: db}
	cashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/refund", strings.NewReader(`{"reason":"customer return"}`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleOrderRefund(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "manager_approval_required") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSVoid); err != nil {
		t.Fatalf("refund scope mismatch consumed void approval: %v", err)
	}
}
