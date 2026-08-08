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

func TestVoidRequiresManagerApprovalForCashier(t *testing.T) {
	s := &Server{}
	cashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/void", strings.NewReader(`{"reason":"customer changed mind"}`))
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleOrderVoid(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "manager_approval_required") || !strings.Contains(res.Body.String(), permissionPOSVoid) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestVoidRejectsApprovalForDifferentSensitivePermission(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }

	token := "discount-only-approval"
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(token_hash,cashier_user_id,approver_user_id,permission,reason,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?)`,
		hash[:], "cashier-1", "manager-1", permissionPOSDiscount, "discount", now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil { t.Fatal(err) }

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

	// Scope mismatch must not consume the discount approval.
	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSDiscount); err != nil {
		t.Fatalf("mismatched void attempt consumed discount approval: %v", err)
	}
}

func TestDirectVoidPermissionStillRequiresReason(t *testing.T) {
	s := &Server{}
	manager := LocalUserContext{UserID: "manager-1", Permissions: []string{permissionPOSVoid, permissionPOSApprove}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/void", strings.NewReader(`{"reason":""}`))
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, manager))
	res := httptest.NewRecorder()

	s.handleOrderVoid(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "void_reason_required") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
