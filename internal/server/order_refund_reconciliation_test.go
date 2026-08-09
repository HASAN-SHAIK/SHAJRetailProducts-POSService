package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/refunds"
)

type fakeRefundReconciliationReader struct {
	snapshot refunds.ReconciliationSnapshot
	err      error
}

func (f fakeRefundReconciliationReader) GetReconciliationSnapshot(context.Context, string) (refunds.ReconciliationSnapshot, error) {
	return f.snapshot, f.err
}

func TestRefundReconciliationHandlerReturnsReadOnlyFacts(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/reconciliation", nil)
	req.SetPathValue("id", "ord-1")
	res := httptest.NewRecorder()

	handleOrderRefundReconciliationWith(fakeRefundReconciliationReader{snapshot: refunds.ReconciliationSnapshot{
		OrderID: "ord-1", OrderStatus: "paid", CapturedPaymentMinor: 10000, ReversedPaymentMinor: 2500,
		SaleIssuedQuantityMilli: 1000, RestoredQuantityMilli: 250,
		PartialReturnOperations: 1, PartialReturnRefundMinor: 2500,
	}})(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	for _, expected := range []string{
		`"order_id":"ord-1"`, `"order_status":"paid"`, `"captured_payment_minor":10000`,
		`"reversed_payment_minor":2500`, `"sale_issued_quantity_milli":1000`,
		`"restored_quantity_milli":250`, `"partial_return_operations":1`,
		`"partial_return_refund_minor":2500`,
	} {
		if !strings.Contains(res.Body.String(), expected) {
			t.Fatalf("body=%s missing=%s", res.Body.String(), expected)
		}
	}
}

func TestRefundReconciliationHandlerMapsReadErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"invalid": {refunds.ErrInvalidPartialReturn, http.StatusBadRequest},
		"missing": {orders.ErrNotFound, http.StatusNotFound},
		"failure": {errors.New("db unavailable"), http.StatusInternalServerError},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/reconciliation", nil)
			req.SetPathValue("id", "ord-1")
			res := httptest.NewRecorder()
			handleOrderRefundReconciliationWith(fakeRefundReconciliationReader{err: tc.err})(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
		})
	}
}

func TestRefundReconciliationHandlerRequiresOrdersReadPermission(t *testing.T) {
	handler := requirePermission("orders:read", handleOrderRefundReconciliationWith(fakeRefundReconciliationReader{}))

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/reconciliation", nil)
	unauthorized.SetPathValue("id", "ord-1")
	unauthorized = unauthorized.WithContext(context.WithValue(unauthorized.Context(), authContextKey{}, LocalUserContext{UserID: "cashier-1", Permissions: []string{"pos:sale"}}))
	unauthorizedRes := httptest.NewRecorder()
	handler(unauthorizedRes, unauthorized)
	if unauthorizedRes.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status=%d body=%s", unauthorizedRes.Code, unauthorizedRes.Body.String())
	}

	authorized := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/reconciliation", nil)
	authorized.SetPathValue("id", "ord-1")
	authorized = authorized.WithContext(context.WithValue(authorized.Context(), authContextKey{}, LocalUserContext{UserID: "cashier-1", Permissions: []string{"orders:read"}}))
	authorizedRes := httptest.NewRecorder()
	handler(authorizedRes, authorized)
	if authorizedRes.Code != http.StatusOK {
		t.Fatalf("authorized status=%d body=%s", authorizedRes.Code, authorizedRes.Body.String())
	}
}
