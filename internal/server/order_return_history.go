package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/refunds"
)

type partialReturnHistoryReader interface {
	ListPartialReturns(context.Context, string) ([]refunds.PartialReturnLedgerRecord, error)
}

func (s *Server) handleOrderReturnHistory(w http.ResponseWriter, r *http.Request) {
	reader := refunds.New(s.db, s.orders, s.payments, s.inventory)
	handleOrderReturnHistoryWith(reader)(w, r)
}

func handleOrderReturnHistoryWith(reader partialReturnHistoryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := strings.TrimSpace(r.PathValue("id"))
		items, err := reader.ListPartialReturns(r.Context(), orderID)
		switch {
		case errors.Is(err, refunds.ErrInvalidPartialReturn):
			writeError(w, http.StatusBadRequest, "invalid_order_id")
			return
		case errors.Is(err, orders.ErrNotFound):
			writeError(w, http.StatusNotFound, "order_not_found")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "return_history_lookup_failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"items": items,
			"count": len(items),
		})
	}
}
