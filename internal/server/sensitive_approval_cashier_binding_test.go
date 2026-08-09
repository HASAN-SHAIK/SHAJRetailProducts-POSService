package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRefundApprovalIsBoundToRequestingCashierAndNotConsumedOnMismatch(t *testing.T) {
	ctx := context.Background()
	db := openSensitiveApprovalDB(t)
	token := "cashier-1-refund-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSRefund)

	s := &Server{db: db}
	otherCashier := LocalUserContext{UserID: "cashier-2", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/refund", strings.NewReader(`{"reason":"customer return"}`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, otherCashier))
	res := httptest.NewRecorder()

	s.handleOrderRefund(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "manager_approval_required") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err != nil {
		t.Fatalf("cashier mismatch consumed refund approval: %v", err)
	}
}

func TestPartialRefundApprovalIsBoundToRequestingCashierAndNotConsumedOnMismatch(t *testing.T) {
	ctx := context.Background()
	db := openSensitiveApprovalDB(t)
	token := "cashier-1-partial-refund-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSRefund)

	s := &Server{db: db}
	otherCashier := LocalUserContext{UserID: "cashier-2", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/refund", strings.NewReader(`{"reason":"damaged","return_id":"ret-1","lines":[{"order_item_id":"item-1","quantity_milli":250}]}`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, otherCashier))
	res := httptest.NewRecorder()

	s.handleOrderRefund(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "manager_approval_required") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err != nil {
		t.Fatalf("cashier mismatch consumed partial-refund approval: %v", err)
	}
}

func TestVoidApprovalIsBoundToRequestingCashierAndNotConsumedOnMismatch(t *testing.T) {
	ctx := context.Background()
	db := openSensitiveApprovalDB(t)
	token := "cashier-1-void-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSVoid)

	s := &Server{db: db}
	otherCashier := LocalUserContext{UserID: "cashier-2", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/void", strings.NewReader(`{"reason":"cancel order"}`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, otherCashier))
	res := httptest.NewRecorder()

	s.handleOrderVoid(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "manager_approval_required") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSVoid); err != nil {
		t.Fatalf("cashier mismatch consumed void approval: %v", err)
	}
}
