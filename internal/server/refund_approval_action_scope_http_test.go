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

func TestRefundApprovalHTTPRejectsWrongActionWithoutBurningToken(t *testing.T) {
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
		cashierID = "cashier-action-http"
		orderID   = "order-action-http"
	)
	now := time.Now().UTC()
	s := &Server{db: db}
	cashier := LocalUserContext{UserID: cashierID, Permissions: []string{permissionPOSSale}}

	attempts := []struct {
		name          string
		token         string
		approvedScope string
		requestBody   string
		requiredScope string
	}{
		{
			name:          "partial approval presented to full refund",
			token:         "refund-action-http-partial",
			approvedScope: approvalActionRefundPartial,
			requestBody:   `{"reason":"cashier reason"}`,
			requiredScope: approvalActionRefundFull,
		},
		{
			name:          "full approval presented to partial refund",
			token:         "refund-action-http-full",
			approvedScope: approvalActionRefundFull,
			requestBody:   `{"return_id":"return-action-http","lines":[{"order_item_id":"item-action-http","quantity_milli":250}],"reason":"cashier reason"}`,
			requiredScope: approvalActionRefundPartial,
		},
	}

	for _, tc := range attempts {
		t.Run(tc.name, func(t *testing.T) {
			hash := sha256.Sum256([]byte(tc.token))
			if _, err := db.SQL().ExecContext(ctx, `
				INSERT INTO pos_manager_approvals(
					token_hash,cashier_user_id,approver_user_id,permission,reason,order_id,action_scope,created_at,expires_at
				) VALUES(?,?,?,?,?,?,?,?,?)`,
				hash[:], cashierID, "manager-action-http", permissionPOSRefund, "approved scoped refund", orderID, tc.approvedScope,
				now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatalf("seed approval: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID+"/refund", strings.NewReader(tc.requestBody))
			req.SetPathValue("id", orderID)
			req.Header.Set("X-POS-Approval-Token", tc.token)
			req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
			res := httptest.NewRecorder()

			s.handleOrderRefund(res, req)

			if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "manager_approval_required") {
				t.Fatalf("wrong-action refund status=%d body=%s", res.Code, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), `"required_action_scope":"`+tc.requiredScope+`"`) {
				t.Fatalf("wrong-action response did not expose required scope %q: %s", tc.requiredScope, res.Body.String())
			}

			var consumedAt *string
			if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
				t.Fatal(err)
			}
			if consumedAt != nil {
				t.Fatalf("wrong-action attempt burned approval: consumed_at=%v", *consumedAt)
			}

			approval, err := s.consumeManagerApprovalForRefundAction(ctx, tc.token, cashierID, orderID, tc.approvedScope)
			if err != nil {
				t.Fatalf("matching action could not consume preserved approval: %v", err)
			}
			if approval.ApproverUserID != "manager-action-http" || approval.Reason != "approved scoped refund" {
				t.Fatalf("unexpected preserved approval: %+v", approval)
			}
			if _, err := s.consumeManagerApprovalForRefundAction(ctx, tc.token, cashierID, orderID, tc.approvedScope); err == nil {
				t.Fatal("matching action approval remained reusable after consumption")
			}
		})
	}
}
