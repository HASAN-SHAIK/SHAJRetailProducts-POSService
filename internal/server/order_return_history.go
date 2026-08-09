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

type partialReturnHistoryLineResponse struct {
	OrderItemID   string `json:"order_item_id"`
	QuantityMilli int64  `json:"quantity_milli"`
	RefundMinor   int64  `json:"refund_minor"`
}

type partialReturnHistoryResponse struct {
	ReturnID         string                             `json:"return_id"`
	OrderID          string                             `json:"order_id"`
	ApprovedByUserID string                             `json:"approved_by_user_id"`
	Reason           string                             `json:"reason"`
	RefundMinor      int64                              `json:"refund_minor"`
	CreatedAt        string                             `json:"created_at"`
	Lines            []partialReturnHistoryLineResponse `json:"lines"`
}

func (s *Server) handleOrderReturnHistory(w http.ResponseWriter, r *http.Request) {
	reader := refunds.New(s.db, s.orders, s.payments, s.inventory)
	handleOrderReturnHistoryWith(reader)(w, r)
}

func handleOrderReturnHistoryWith(reader partialReturnHistoryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := strings.TrimSpace(r.PathValue("id"))
		records, err := reader.ListPartialReturns(r.Context(), orderID)
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

		items := make([]partialReturnHistoryResponse, 0, len(records))
		for _, record := range records {
			item := partialReturnHistoryResponse{
				ReturnID:         record.ID,
				OrderID:          record.OrderID,
				ApprovedByUserID: record.ApprovedByUserID,
				Reason:           record.Reason,
				RefundMinor:      record.RefundMinor,
				CreatedAt:        record.CreatedAt,
				Lines:            make([]partialReturnHistoryLineResponse, 0, len(record.Lines)),
			}
			for _, line := range record.Lines {
				item.Lines = append(item.Lines, partialReturnHistoryLineResponse{
					OrderItemID: line.OrderItemID,
					QuantityMilli: line.QuantityMilli,
					RefundMinor: line.RefundMinor,
				})
			}
			items = append(items, item)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"items": items,
			"count": len(items),
		})
	}
}
