package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/localauth"
)

func TestFullRefundManagerApprovalAPIStoresExactCentralAuthorizedScope(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	privateKey, publicPEM := refundApprovalTestKeys(t)
	localAuth := localauth.New(db, publicPEM)
	grant := refundApprovalTestGrant(t, privateKey, map[string]any{
		"type":              "pos_offline_grant",
		"user_id":           "manager-full-refund-scope",
		"tenant_id":         "tenant-full-refund-scope",
		"role":              "manager",
		"branch_id":         "branch-full-refund-scope",
		"all_branch_access": false,
		"permissions":       []string{permissionPOSApprove, permissionPOSRefund},
		"grant_id":          "grant-full-refund-scope",
		"iss":               "shajtech-central",
		"aud":               "shajtech-pos-edge",
		"exp":               time.Now().Add(time.Hour).Unix(),
	})
	if _, err := localAuth.Enroll(ctx, grant, "8642"); err != nil {
		t.Fatalf("enroll Central-authorized manager: %v", err)
	}

	s := &Server{db: db, localAuth: localAuth}
	cashier := LocalUserContext{
		UserID:      "cashier-full-refund-scope",
		TenantID:    "tenant-full-refund-scope",
		BranchID:    "branch-full-refund-scope",
		Permissions: []string{permissionPOSSale},
	}
	body := `{"manager_user_id":"manager-full-refund-scope","pin":"8642","permission":"pos:refund","reason":"customer returned entire order","order_id":"order-full-refund-scope","action_scope":"refund_full"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/approvals", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleManagerApproval(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("approval issuance status=%d body=%s", res.Code, res.Body.String())
	}

	var issued struct {
		ApprovalToken  string `json:"approval_token"`
		ApproverUserID string `json:"approver_user_id"`
		Permission     string `json:"permission"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode approval response: %v", err)
	}
	if issued.ApprovalToken == "" || issued.ApproverUserID != "manager-full-refund-scope" || issued.Permission != permissionPOSRefund {
		t.Fatalf("unexpected approval response: %+v", issued)
	}

	tokenHash := sha256.Sum256([]byte(issued.ApprovalToken))
	var orderID, actionScope, reason string
	var consumedAt sql.NullString
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT order_id,action_scope,reason,consumed_at
		FROM pos_manager_approvals
		WHERE token_hash=?`, tokenHash[:]).Scan(&orderID, &actionScope, &reason, &consumedAt); err != nil {
		t.Fatalf("read issued approval: %v", err)
	}
	if orderID != "order-full-refund-scope" || actionScope != approvalActionRefundFull || reason != "customer returned entire order" || consumedAt.Valid {
		t.Fatalf("unexpected persisted approval order=%q action=%q reason=%q consumed=%v", orderID, actionScope, reason, consumedAt.Valid)
	}

	if _, err := s.consumeManagerApprovalForRefundAction(ctx, issued.ApprovalToken, cashier.UserID, orderID, approvalActionRefundPartial); err == nil {
		t.Fatal("full-refund approval authorized a partial refund")
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, tokenHash[:]).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt.Valid {
		t.Fatal("wrong-action attempt burned the issued full-refund approval")
	}

	approval, err := s.consumeManagerApprovalForRefundAction(ctx, issued.ApprovalToken, cashier.UserID, orderID, approvalActionRefundFull)
	if err != nil {
		t.Fatalf("consume matching full-refund approval: %v", err)
	}
	if approval.ApproverUserID != "manager-full-refund-scope" || approval.Reason != "customer returned entire order" {
		t.Fatalf("unexpected consumed approval: %+v", approval)
	}
	if _, err := s.consumeManagerApprovalForRefundAction(ctx, issued.ApprovalToken, cashier.UserID, orderID, approvalActionRefundFull); err == nil {
		t.Fatal("issued full-refund approval remained reusable after one matching consumption")
	}
}
