package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/syncrecovery"
)

type syncRecoveryDeviceReader interface {
	Get(context.Context) (device.Identity, error)
}

type syncRecoveryOutbox interface {
	RequeueDeadLetter(context.Context, string, string) error
}

type syncRecoveryGrantVerifier func(string, string) (syncrecovery.Grant, error)

type syncRecoveryRequest struct {
	RecoveryGrant string `json:"recovery_grant"`
}

func (s *Server) handleOrderSyncRecovery(w http.ResponseWriter, r *http.Request) {
	handleOrderSyncRecoveryWith(
		s.cfg.CentralTenantID,
		s.cfg.OfflineGrantSecret,
		s.device,
		outbox.New(s.db),
		syncrecovery.Verify,
	)(w, r)
}

func handleOrderSyncRecoveryWith(tenantID, publicKey string, devices syncRecoveryDeviceReader, events syncRecoveryOutbox, verify syncRecoveryGrantVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := strings.TrimSpace(r.PathValue("id"))
		if orderID == "" {
			writeError(w, http.StatusBadRequest, "invalid_order_id")
			return
		}

		var input syncRecoveryRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&input); err != nil || strings.TrimSpace(input.RecoveryGrant) == "" {
			writeError(w, http.StatusBadRequest, "invalid_sync_recovery_payload")
			return
		}

		grant, err := verify(strings.TrimSpace(input.RecoveryGrant), publicKey)
		if err != nil {
			writeError(w, http.StatusForbidden, "sync_recovery_not_authorized")
			return
		}

		identity, err := devices.Get(r.Context())
		if err != nil {
			writeError(w, http.StatusConflict, "device_identity_unavailable")
			return
		}
		orderingKey := "sales_order:" + orderID
		if grant.TenantID != strings.TrimSpace(tenantID) || grant.DeviceID != identity.DeviceID || grant.OrderID != orderID || grant.OrderingKey != orderingKey {
			writeError(w, http.StatusForbidden, "sync_recovery_scope_mismatch")
			return
		}

		if err := events.RequeueDeadLetter(r.Context(), grant.EventID, orderingKey); err != nil {
			switch {
			case errors.Is(err, outbox.ErrDeadLetterNotFound):
				writeError(w, http.StatusConflict, "sync_recovery_target_unavailable")
			case errors.Is(err, outbox.ErrRecoveryOrderingMismatch):
				writeError(w, http.StatusForbidden, "sync_recovery_scope_mismatch")
			default:
				writeError(w, http.StatusInternalServerError, "sync_recovery_failed")
			}
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":              "requeued",
			"recovery_id":         grant.RecoveryID,
			"event_id":            grant.EventID,
			"order_id":            grant.OrderID,
			"ordering_key":        grant.OrderingKey,
			"approved_by_user_id": grant.ApprovedByUserID,
			"reason":              grant.Reason,
		})
	}
}
