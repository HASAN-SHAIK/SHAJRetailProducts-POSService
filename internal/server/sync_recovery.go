package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/syncrecovery"
)

type syncRecoveryInput struct {
	Grant string `json:"grant"`
}

// handleSyncRecovery requeues only the exact dead-letter head authorized by a
// Central-signed recovery grant. Central owns authorization; POS only verifies
// the grant against its local tenant/device/order scope, durably consumes that
// authorization once, and performs the minimum exact-event state transition.
func (s *Server) handleSyncRecovery(w http.ResponseWriter, r *http.Request) {
	var input syncRecoveryInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil || strings.TrimSpace(input.Grant) == "" {
		writeError(w, http.StatusBadRequest, "invalid_sync_recovery_payload")
		return
	}

	grant, err := syncrecovery.Verify(input.Grant, s.cfg.OfflineGrantSecret)
	if err != nil {
		writeError(w, http.StatusForbidden, "sync_recovery_grant_invalid")
		return
	}

	orderID := strings.TrimSpace(r.PathValue("id"))
	identity, err := s.device.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, "device_identity_unavailable")
		return
	}
	if s.cfg.CentralTenantID == "" || grant.TenantID != s.cfg.CentralTenantID || grant.DeviceID != identity.DeviceID || grant.OrderID != orderID || grant.OrderingKey != "sales_order:"+orderID {
		writeError(w, http.StatusForbidden, "sync_recovery_scope_mismatch")
		return
	}

	err = outbox.New(s.db).ApplyAuthorizedRecovery(r.Context(), outbox.RecoveryAuthorization{
		RecoveryID: grant.RecoveryID,
		EventID: grant.EventID,
		OrderingKey: grant.OrderingKey,
		OrderID: grant.OrderID,
		ApprovedByUserID: grant.ApprovedByUserID,
		Reason: grant.Reason,
	})
	if err != nil {
		switch {
		case errors.Is(err, outbox.ErrRecoveryAlreadyConsumed):
			writeError(w, http.StatusConflict, "sync_recovery_grant_consumed")
		case errors.Is(err, outbox.ErrDeadLetterNotFound):
			writeError(w, http.StatusConflict, "sync_recovery_not_available")
		case errors.Is(err, outbox.ErrRecoveryOrderingMismatch), errors.Is(err, outbox.ErrInvalidRecoveryAuthorization):
			writeError(w, http.StatusForbidden, "sync_recovery_scope_mismatch")
		default:
			writeError(w, http.StatusInternalServerError, "sync_recovery_failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recovery_id": grant.RecoveryID,
		"event_id": grant.EventID,
		"order_id": grant.OrderID,
		"status": "pending",
		"approved_by_user_id": grant.ApprovedByUserID,
		"reason": grant.Reason,
	})
}
