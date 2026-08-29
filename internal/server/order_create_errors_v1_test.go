package server

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func TestV1OrderCreateErrorContract(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"customer missing", orders.ErrCustomerNotFound, http.StatusBadRequest, "customer_not_found"},
		{"product missing", fmt.Errorf("load product 24: %w", catalog.ErrNotFound), http.StatusConflict, "product_not_found"},
		{"price override denied", orders.ErrPriceOverrideNotAllowed, http.StatusUnprocessableEntity, "price_override_not_allowed"},
		{"discount disabled", orders.ErrDiscountNotAllowed, http.StatusUnprocessableEntity, "discount_not_allowed"},
		{"discount limit", orders.ErrDiscountLimitExceeded, http.StatusUnprocessableEntity, "discount_limit_exceeded"},
		{"pricing config unavailable", errors.Join(errors.New("config read failed"), orders.ErrPricingPolicyUnavailable), http.StatusServiceUnavailable, "pricing_policy_unavailable"},
		{"tax config unavailable", errors.Join(errors.New("config read failed"), orders.ErrTaxPolicyUnavailable), http.StatusServiceUnavailable, "tax_policy_unavailable"},
		{"invalid tax policy", orders.ErrInvalidTaxPolicy, http.StatusServiceUnavailable, "tax_policy_invalid"},
		{"invalid order", orders.ErrInvalidOrder, http.StatusBadRequest, "invalid_order"},
		{"unexpected create error", errors.New("sqlite unavailable"), http.StatusBadRequest, "order_create_failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code := classifyOrderCreateError(tt.err)
			if status != tt.status || code != tt.code {
				t.Fatalf("got status=%d code=%q, want status=%d code=%q", status, code, tt.status, tt.code)
			}
		})
	}
}
