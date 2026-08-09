package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSyncRecoveryRejectsMalformedPayload(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/sync-recovery", strings.NewReader(`{"grant":""}`))
	req.SetPathValue("id", "ord-1")
	res := httptest.NewRecorder()

	s.handleSyncRecovery(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "invalid_sync_recovery_payload") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestSyncRecoveryRejectsUnsignedGrantBeforeAnyMutation(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-1/sync-recovery", strings.NewReader(`{"grant":"not-a-signed-central-grant"}`))
	req.SetPathValue("id", "ord-1")
	res := httptest.NewRecorder()

	s.handleSyncRecovery(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "sync_recovery_grant_invalid") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
