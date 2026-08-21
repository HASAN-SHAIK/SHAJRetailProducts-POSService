package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

type voidOrderInput struct {
	Reason string `json:"reason"`
}

func (s *Server) handleOrderVoid(w http.ResponseWriter, r *http.Request) {
	user, ok := localUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "local_session_required")
		return
	}

	var input voidOrderInput
	if r.Body != nil {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_void_payload")
			return
		}
	}
	input.Reason = strings.TrimSpace(input.Reason)
	orderID := strings.TrimSpace(r.PathValue("id"))

	approverUserID := ""
	reason := input.Reason
	if hasLocalPermission(user, permissionPOSVoid) {
		approverUserID = user.UserID
	} else {
		approval, err := s.consumeManagerApprovalForOrder(r.Context(), r.Header.Get("X-POS-Approval-Token"), user.UserID, permissionPOSVoid, orderID)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "manager_approval_required",
				"required_permission": permissionPOSVoid,
			})
			return
		}
		approverUserID = approval.ApproverUserID
		reason = strings.TrimSpace(approval.Reason)
	}

	if strings.TrimSpace(user.UserID) == "" || approverUserID == "" || reason == "" {
		writeError(w, http.StatusBadRequest, "void_reason_required")
		return
	}

	order, err := s.orders.VoidWithActors(r.Context(), orderID, user.UserID, approverUserID, reason)
	switch {
	case errors.Is(err, orders.ErrNotFound):
		writeError(w, http.StatusNotFound, "order_not_found")
		return
	case errors.Is(err, orders.ErrAlreadyVoided):
		writeError(w, http.StatusConflict, "order_already_voided")
		return
	case errors.Is(err, orders.ErrRefundRequired):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "refund_required",
			"required_permission": permissionPOSRefund,
		})
		return
	case errors.Is(err, orders.ErrPaymentReversalRequired):
		writeError(w, http.StatusConflict, "payment_reversal_required")
		return
	case errors.Is(err, orders.ErrInvalidOrder):
		writeError(w, http.StatusBadRequest, "invalid_void")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "order_void_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"order": order,
		"voided_by_user_id": user.UserID,
		"approved_by_user_id": approverUserID,
		"reason": reason,
	})
}
