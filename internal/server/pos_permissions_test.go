package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPOSCashierCanRunOrdinarySale(t *testing.T) {
	user := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	nextCalled := false
	handler := requirePermission("orders:write", func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"items":[{"discount_minor":0}]}`))
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, user))
	res := httptest.NewRecorder()

	handler(res, req)
	if !nextCalled || res.Code != http.StatusNoContent {
		t.Fatalf("ordinary sale denied: called=%v status=%d body=%s", nextCalled, res.Code, res.Body.String())
	}
}

func TestPOSDiscountRequiresDedicatedPermission(t *testing.T) {
	user := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	handler := requirePermission("orders:write", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("discounted sale reached business handler without pos:discount")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"items":[{"discount_minor":250}]}`))
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, user))
	res := httptest.NewRecorder()

	handler(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "manager_approval_required") {
		t.Fatalf("discount response status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestPOSDiscountAllowedWithPermission(t *testing.T) {
	user := LocalUserContext{UserID: "manager-1", Permissions: []string{permissionPOSSale, permissionPOSDiscount, permissionPOSApprove}}
	nextCalled := false
	handler := requirePermission("orders:write", func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"items":[{"discount_minor":250}]}`))
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, user))
	res := httptest.NewRecorder()

	handler(res, req)
	if !nextCalled || res.Code != http.StatusNoContent {
		t.Fatalf("manager discount denied: called=%v status=%d body=%s", nextCalled, res.Code, res.Body.String())
	}
}

func TestLegacyOrdersWriteStillAllowsBasicSaleOnly(t *testing.T) {
	legacy := LocalUserContext{UserID: "legacy-staff", Permissions: []string{"orders:write"}}
	handler := requirePermission("orders:write", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	basic := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"items":[{"discount_minor":0}]}`))
	basic = basic.WithContext(context.WithValue(basic.Context(), authContextKey{}, legacy))
	basicRes := httptest.NewRecorder()
	handler(basicRes, basic)
	if basicRes.Code != http.StatusNoContent { t.Fatalf("legacy basic sale status=%d", basicRes.Code) }

	discount := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"items":[{"discount_minor":1}]}`))
	discount = discount.WithContext(context.WithValue(discount.Context(), authContextKey{}, legacy))
	discountRes := httptest.NewRecorder()
	handler(discountRes, discount)
	if discountRes.Code != http.StatusForbidden { t.Fatalf("legacy discount status=%d", discountRes.Code) }
}
