package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMalformedRefundPayloadDoesNotConsumeManagerApproval(t *testing.T) {
	ctx := context.Background()
	db := openSensitiveApprovalDB(t)
	token := "refund-preflight-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSRefund)

	s := &Server{db: db}
	cashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/refund", strings.NewReader(`{"reason":`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleOrderRefund(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "invalid_refund_payload") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err != nil {
		t.Fatalf("malformed refund payload consumed approval: %v", err)
	}
}

func TestInvalidPartialRefundShapeDoesNotConsumeManagerApproval(t *testing.T) {
	ctx := context.Background()
	db := openSensitiveApprovalDB(t)
	token := "partial-refund-preflight-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSRefund)

	s := &Server{db: db}
	cashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/refund", strings.NewReader(`{"reason":"damaged","lines":[{"order_item_id":"item-1","quantity_milli":250}]}`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleOrderRefund(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "invalid_partial_refund") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err != nil {
		t.Fatalf("invalid partial refund shape consumed approval: %v", err)
	}
}

func TestMalformedVoidPayloadDoesNotConsumeManagerApproval(t *testing.T) {
	ctx := context.Background()
	db := openSensitiveApprovalDB(t)
	token := "void-preflight-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSVoid)

	s := &Server{db: db}
	cashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/void", strings.NewReader(`{"reason":`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleOrderVoid(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "invalid_void_payload") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSVoid); err != nil {
		t.Fatalf("malformed void payload consumed approval: %v", err)
	}
}
