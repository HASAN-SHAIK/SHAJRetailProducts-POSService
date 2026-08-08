package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/refunds"
)

type refundOrderInput struct {
	Reason string `json:"reason"`
}

func (s *Server) handleOrderRefund(w http.ResponseWriter, r *http.Request) {
	user, ok := localUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "local_session_required")
		return
	}

	var input refundOrderInput
	if r.Body != nil {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_refund_payload")
			return
		}
	}
	input.Reason = strings.TrimSpace(input.Reason)

	approverUserID := ""
	reason := input.Reason
	if hasLocalPermission(user, permissionPOSRefund) {
		approverUserID = user.UserID
	} else {
		approval, err := s.consumeManagerApproval(r.Context(), r.Header.Get("X-POS-Approval-Token"), user.UserID, permissionPOSRefund)
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
	order, err := refundService.RefundFullSale(r.Context(), r.PathValue("id"), approverUserID, reason)
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
