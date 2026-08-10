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

func TestRefundApprovalHTTPRejectsWrongOrderWithoutBurningToken(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const (
		token       = "refund-http-order-scoped-approval"
		cashierID   = "cashier-1"
		approvedID  = "order-approved"
		attemptedID = "order-wrong"
	)
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(
			token_hash,cashier_user_id,approver_user_id,permission,reason,order_id,created_at,expires_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		hash[:], cashierID, "manager-1", permissionPOSRefund, "approved refund", approvedID,
		now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed approval: %v", err)
	}

	s := &Server{db: db}
	cashier := LocalUserContext{UserID: cashierID, Permissions: []string{permissionPOSSale}}

	attempts := []struct {
		name string
		body string
	}{
		{name: "full refund", body: `{"reason":"cashier reason"}`},
		{name: "partial refund", body: `{"return_id":"return-wrong-order","lines":[{"order_item_id":"item-1","quantity_milli":250}],"reason":"cashier reason"}`},
	}

	for _, tc := range attempts {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+attemptedID+"/refund", strings.NewReader(tc.body))
			req.SetPathValue("id", attemptedID)
			req.Header.Set("X-POS-Approval-Token", token)
			req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
			res := httptest.NewRecorder()

			s.handleOrderRefund(res, req)

			if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "manager_approval_required") {
				t.Fatalf("wrong-order refund status=%d body=%s", res.Code, res.Body.String())
			}

			var consumedAt *string
			if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
				t.Fatal(err)
			}
			if consumedAt != nil {
				t.Fatalf("wrong-order %s burned approval: consumed_at=%v", tc.name, *consumedAt)
			}
		})
	}

	approval, err := s.consumeManagerApprovalForOrder(ctx, token, cashierID, permissionPOSRefund, approvedID)
	if err != nil {
		t.Fatalf("correct order could not consume preserved approval: %v", err)
	}
	if approval.ApproverUserID != "manager-1" || approval.Reason != "approved refund" {
		t.Fatalf("unexpected approval after wrong-order attempts: %+v", approval)
	}
	if _, err := s.consumeManagerApprovalForOrder(ctx, token, cashierID, permissionPOSRefund, approvedID); err == nil {
		t.Fatal("correct-order approval remained reusable after consumption")
	}
}
