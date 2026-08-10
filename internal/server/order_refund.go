package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/refunds"
)

type refundOrderLineInput struct {
	OrderItemID   string `json:"order_item_id"`
	QuantityMilli int64  `json:"quantity_milli"`
}

type refundOrderInput struct {
	Reason   string                 `json:"reason"`
	ReturnID string                 `json:"return_id,omitempty"`
	Lines    []refundOrderLineInput `json:"lines,omitempty"`
}

func (s *Server) handleOrderRefund(w http.ResponseWriter, r *http.Request) {
	user, ok := localUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "local_session_required")
		return
	}

	var input refundOrderInput
	if r.Body != nil {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_refund_payload")
			return
		}
	}
	input.Reason = strings.TrimSpace(input.Reason)
	input.ReturnID = strings.TrimSpace(input.ReturnID)
	orderID := strings.TrimSpace(r.PathValue("id"))

	partial := input.ReturnID != "" || len(input.Lines) > 0
	partialLines := make([]refunds.PartialReturnLineInput, 0, len(input.Lines))
	if partial {
		if input.ReturnID == "" || len(input.Lines) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_partial_refund")
			return
		}
		for _, line := range input.Lines {
			itemID := strings.TrimSpace(line.OrderItemID)
			if itemID == "" || line.QuantityMilli <= 0 {
				writeError(w, http.StatusBadRequest, "invalid_partial_refund")
				return
			}
			partialLines = append(partialLines, refunds.PartialReturnLineInput{
				OrderItemID: itemID,
				QuantityMilli: line.QuantityMilli,
			})
		}
	}

	approverUserID := ""
	reason := input.Reason
	if hasLocalPermission(user, permissionPOSRefund) {
		approverUserID = user.UserID
	} else {
		approval, err := s.consumeManagerApprovalForOrder(r.Context(), r.Header.Get("X-POS-Approval-Token"), user.UserID, permissionPOSRefund, orderID)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "manager_approval_required",
				"required_permission": permissionPOSRefund,
			})
			return
		}
		approverUserID = approval.ApproverUserID
		reason = strings.TrimSpace(approval.Reason)
	}

	if approverUserID == "" || reason == "" {
		writeError(w, http.StatusBadRequest, "refund_reason_required")
		return
	}

	refundService := refunds.New(s.db, s.orders, s.payments, s.inventory)
	if partial {
		order, plan, err := refundService.ReturnPartial(r.Context(), refunds.PartialReturnInput{
			ReturnID: input.ReturnID,
			OrderID: orderID,
			ApprovedByUserID: approverUserID,
			Reason: reason,
			Lines: partialLines,
		})
		switch {
		case errors.Is(err, orders.ErrNotFound):
			writeError(w, http.StatusNotFound, "order_not_found")
			return
		case errors.Is(err, refunds.ErrReturnQuantityExceeded):
			writeError(w, http.StatusConflict, "return_quantity_exceeded")
			return
		case errors.Is(err, refunds.ErrPartialReturnReplayMismatch):
			writeError(w, http.StatusConflict, "partial_refund_replay_mismatch")
			return
		case errors.Is(err, refunds.ErrExistingReversal):
			writeError(w, http.StatusConflict, "refund_reconciliation_required")
			return
		case errors.Is(err, refunds.ErrInvalidPartialReturn), errors.Is(err, orders.ErrInvalidOrder):
			writeError(w, http.StatusBadRequest, "invalid_partial_refund")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "order_partial_refund_failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"order": order,
			"return_id": input.ReturnID,
			"plan": plan,
			"refunded_by_user_id": approverUserID,
			"reason": reason,
		})
		return
	}

	order, err := refundService.RefundFullSale(r.Context(), orderID, approverUserID, reason)
	switch {
	case errors.Is(err, orders.ErrNotFound):
		writeError(w, http.StatusNotFound, "order_not_found")
		return
	case errors.Is(err, orders.ErrAlreadyReturned):
		writeError(w, http.StatusConflict, "order_already_returned")
		return
	case errors.Is(err, orders.ErrNotCompleted):
		writeError(w, http.StatusConflict, "order_not_completed")
		return
	case errors.Is(err, refunds.ErrExistingReversal):
		writeError(w, http.StatusConflict, "refund_reconciliation_required")
		return
	case errors.Is(err, orders.ErrInvalidOrder):
		writeError(w, http.StatusBadRequest, "invalid_refund")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "order_refund_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"order": order,
		"refunded_by_user_id": approverUserID,
		"reason": reason,
	})
}
