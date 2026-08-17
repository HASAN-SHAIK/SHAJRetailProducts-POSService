package server

import (
	"errors"
	"net/http"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func classifyOrderCreateError(err error) (int, string) {
	switch {
	case errors.Is(err, orders.ErrPriceOverrideNotAllowed):
		return http.StatusUnprocessableEntity, "price_override_not_allowed"
	case errors.Is(err, orders.ErrDiscountNotAllowed):
		return http.StatusUnprocessableEntity, "discount_not_allowed"
	case errors.Is(err, orders.ErrDiscountLimitExceeded):
		return http.StatusUnprocessableEntity, "discount_limit_exceeded"
	case errors.Is(err, orders.ErrPricingPolicyUnavailable):
		return http.StatusServiceUnavailable, "pricing_policy_unavailable"
	case errors.Is(err, orders.ErrTaxPolicyUnavailable):
		return http.StatusServiceUnavailable, "tax_policy_unavailable"
	case errors.Is(err, orders.ErrInvalidTaxPolicy):
		return http.StatusServiceUnavailable, "tax_policy_invalid"
	case errors.Is(err, orders.ErrInvalidOrder):
		return http.StatusBadRequest, "invalid_order"
	default:
		return http.StatusBadRequest, "order_create_failed"
	}
}
