package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/refunds"
)

type refundReconciliationReader interface {
	GetReconciliationSnapshot(context.Context, string) (refunds.ReconciliationSnapshot, error)
}

func (s *Server) handleOrderRefundReconciliation(w http.ResponseWriter, r *http.Request) {
	reader := refunds.New(s.db, s.orders, s.payments, s.inventory)
	handleOrderRefundReconciliationWith(reader)(w, r)
}

func handleOrderRefundReconciliationWith(reader refundReconciliationReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := strings.TrimSpace(r.PathValue("id"))
		snapshot, err := reader.GetReconciliationSnapshot(r.Context(), orderID)
		switch {
		case errors.Is(err, refunds.ErrInvalidPartialReturn):
			writeError(w, http.StatusBadRequest, "invalid_order_id")
			return
		case errors.Is(err, orders.ErrNotFound):
			writeError(w, http.StatusNotFound, "order_not_found")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "refund_reconciliation_lookup_failed")
			return
		}

		writeJSON(w, http.StatusOK, snapshot)
	}
}
