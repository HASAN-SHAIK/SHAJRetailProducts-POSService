package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/syncrecovery"
)

type fakeSyncRecoveryDeviceReader struct {
	identity device.Identity
	err      error
}

func (f fakeSyncRecoveryDeviceReader) Get(context.Context) (device.Identity, error) {
	return f.identity, f.err
}

type fakeSyncRecoveryOutbox struct {
	eventID     string
	orderingKey string
	err         error
	calls       int
}

func (f *fakeSyncRecoveryOutbox) RequeueDeadLetter(_ context.Context, eventID, orderingKey string) error {
	f.calls++
	f.eventID = eventID
	f.orderingKey = orderingKey
	return f.err
}

func validRecoveryGrant() syncrecovery.Grant {
	return syncrecovery.Grant{
		RecoveryID: "recovery-1",
		TenantID: "tenant-1",
		DeviceID: "device-1",
		OrderID: "order-1",
		OrderingKey: "sales_order:order-1",
		EventID: "event-dead-1",
		ApprovedByUserID: "manager-1",
		Reason: "reviewed poisoned refund fact",
	}
}

func recoveryRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/order-1/sync-recovery", strings.NewReader(body))
	req.SetPathValue("id", "order-1")
	return req
}

func TestOrderSyncRecoveryRequiresValidCentralGrantAndExactScope(t *testing.T) {
	cases := map[string]struct {
		grant       syncrecovery.Grant
		verifyErr   error
		device      device.Identity
		wantStatus  int
		wantCalls   int
	}{
		"valid": {
			grant: validRecoveryGrant(),
			device: device.Identity{DeviceID: "device-1"},
			wantStatus: http.StatusOK,
			wantCalls: 1,
		},
		"invalid signature": {
			verifyErr: syncrecovery.ErrInvalidGrant,
			device: device.Identity{DeviceID: "device-1"},
			wantStatus: http.StatusForbidden,
		},
		"wrong tenant": {
			grant: func() syncrecovery.Grant { g := validRecoveryGrant(); g.TenantID = "tenant-2"; return g }(),
			device: device.Identity{DeviceID: "device-1"},
			wantStatus: http.StatusForbidden,
		},
		"wrong device": {
			grant: validRecoveryGrant(),
			device: device.Identity{DeviceID: "device-2"},
			wantStatus: http.StatusForbidden,
		},
		"wrong order": {
			grant: func() syncrecovery.Grant { g := validRecoveryGrant(); g.OrderID = "order-2"; g.OrderingKey = "sales_order:order-2"; return g }(),
			device: device.Identity{DeviceID: "device-1"},
			wantStatus: http.StatusForbidden,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			events := &fakeSyncRecoveryOutbox{}
			verify := func(string, string) (syncrecovery.Grant, error) { return tc.grant, tc.verifyErr }
			res := httptest.NewRecorder()
			handleOrderSyncRecoveryWith("tenant-1", "public-key", fakeSyncRecoveryDeviceReader{identity: tc.device}, events, verify)(res, recoveryRequest(t, `{"recovery_grant":"signed-token"}`))
			if res.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			if events.calls != tc.wantCalls {
				t.Fatalf("requeue calls=%d want=%d", events.calls, tc.wantCalls)
			}
			if tc.wantCalls == 1 && (events.eventID != "event-dead-1" || events.orderingKey != "sales_order:order-1") {
				t.Fatalf("unexpected requeue target event=%s key=%s", events.eventID, events.orderingKey)
			}
		})
	}
}

func TestOrderSyncRecoveryFailsClosedForUnavailableTarget(t *testing.T) {
	events := &fakeSyncRecoveryOutbox{err: outbox.ErrDeadLetterNotFound}
	verify := func(string, string) (syncrecovery.Grant, error) { return validRecoveryGrant(), nil }
	res := httptest.NewRecorder()
	handleOrderSyncRecoveryWith("tenant-1", "public-key", fakeSyncRecoveryDeviceReader{identity: device.Identity{DeviceID: "device-1"}}, events, verify)(res, recoveryRequest(t, `{"recovery_grant":"signed-token"}`))
	if res.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestOrderSyncRecoveryRejectsMalformedPayloadAndDeviceFailure(t *testing.T) {
	verify := func(string, string) (syncrecovery.Grant, error) { return validRecoveryGrant(), nil }

	malformed := httptest.NewRecorder()
	handleOrderSyncRecoveryWith("tenant-1", "public-key", fakeSyncRecoveryDeviceReader{identity: device.Identity{DeviceID: "device-1"}}, &fakeSyncRecoveryOutbox{}, verify)(malformed, recoveryRequest(t, `{}`))
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed status=%d body=%s", malformed.Code, malformed.Body.String())
	}

	deviceFailure := httptest.NewRecorder()
	handleOrderSyncRecoveryWith("tenant-1", "public-key", fakeSyncRecoveryDeviceReader{err: errors.New("device unavailable")}, &fakeSyncRecoveryOutbox{}, verify)(deviceFailure, recoveryRequest(t, `{"recovery_grant":"signed-token"}`))
	if deviceFailure.Code != http.StatusConflict {
		t.Fatalf("device status=%d body=%s", deviceFailure.Code, deviceFailure.Body.String())
	}
}
