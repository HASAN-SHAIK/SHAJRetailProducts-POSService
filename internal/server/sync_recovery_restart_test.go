package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

func signServerRecoveryGrant(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "pos-offline-v1"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(payload)
	input := h + "." + p
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestCentralAuthorizedRecoverySurvivesRestartAndPreservesOrderHead(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pos.db")
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatal(err)
	}

	deviceService := device.New(db)
	identity, err := deviceService.EnsureInstallation(ctx)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))

	orderID := "ord-recovery-restart"
	orderingKey := "sales_order:" + orderID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, event := range []struct {
		id        string
		version   int
		eventType string
		status    string
		attempts  int
	}{
		{"evt-refund-head", 3, "payment.recorded", "dead_letter", 12},
		{"evt-refund-inventory", 4, "inventory.movement.recorded", "pending", 0},
		{"evt-refund-lifecycle", 5, "sale.returned", "pending", 0},
	} {
		if _, err := db.SQL().ExecContext(ctx, `
			INSERT INTO outbox_events(
				id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
				ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at,last_error
			) VALUES(?,?,?,?,?,1,?,'{}','{}',?,?,?,?,?)`,
			event.id, "sales_order", orderID, event.version, event.eventType, orderingKey,
			event.status, event.attempts, now, now, "poisoned",
		); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}

	grant := signServerRecoveryGrant(t, privateKey, map[string]any{
		"type":                "pos_sync_recovery_grant",
		"recovery_id":         "recovery-restart-1",
		"tenant_id":           "tenant-1",
		"device_id":           identity.DeviceID,
		"order_id":            orderID,
		"ordering_key":        orderingKey,
		"event_id":            "evt-refund-head",
		"approved_by_user_id": "manager-central-1",
		"reason":              "reviewed poisoned refund head",
		"iss":                 "shajtech-central",
		"aud":                 "shajtech-pos-edge",
		"exp":                 time.Now().Add(10 * time.Minute).Unix(),
	})

	s := &Server{
		cfg:    config.Config{CentralTenantID: "tenant-1", OfflineGrantSecret: publicPEM},
		db:     db,
		device: deviceService,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID+"/sync-recovery", strings.NewReader(`{"grant":"`+grant+`"}`))
	req.SetPathValue("id", orderID)
	res := httptest.NewRecorder()
	s.handleSyncRecovery(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"status":"pending"`) || !strings.Contains(res.Body.String(), `"event_id":"evt-refund-head"`) {
		db.Close()
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	var headStatus string
	var headAttempts int
	if err := db.SQL().QueryRowContext(ctx, `SELECT status,attempt_count FROM outbox_events WHERE id='evt-refund-head'`).Scan(&headStatus, &headAttempts); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if headStatus != "pending" || headAttempts != 0 {
		db.Close()
		t.Fatalf("requeued head status=%s attempts=%d", headStatus, headAttempts)
	}
	for _, id := range []string{"evt-refund-inventory", "evt-refund-lifecycle"} {
		var status string
		if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM outbox_events WHERE id=?`, id).Scan(&status); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if status != "pending" {
			db.Close()
			t.Fatalf("later event %s changed to %s", id, status)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	restartedDevice := device.New(db)
	restartedIdentity, err := restartedDevice.EnsureInstallation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if restartedIdentity.DeviceID != identity.DeviceID {
		t.Fatalf("device identity changed across restart: before=%s after=%s", identity.DeviceID, restartedIdentity.DeviceID)
	}

	restartedServer := &Server{
		cfg:    config.Config{CentralTenantID: "tenant-1", OfflineGrantSecret: publicPEM},
		db:     db,
		device: restartedDevice,
	}
	replayReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID+"/sync-recovery", strings.NewReader(`{"grant":"`+grant+`"}`))
	replayReq.SetPathValue("id", orderID)
	replayRes := httptest.NewRecorder()
	restartedServer.handleSyncRecovery(replayRes, replayReq)
	if replayRes.Code != http.StatusConflict || !strings.Contains(replayRes.Body.String(), `"error":"sync_recovery_grant_consumed"`) {
		t.Fatalf("replayed recovery status=%d body=%s", replayRes.Code, replayRes.Body.String())
	}

	var recoveryAuditCount int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_sync_recoveries WHERE recovery_id='recovery-restart-1'`).Scan(&recoveryAuditCount); err != nil {
		t.Fatal(err)
	}
	if recoveryAuditCount != 1 {
		t.Fatalf("recovery audit rows=%d want=1 after restart replay", recoveryAuditCount)
	}

	claimed, err := outbox.New(db).ClaimNext(ctx, "restart-recovery-certification")
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != "evt-refund-head" {
		t.Fatalf("claimed=%+v want recovered poisoned head", claimed)
	}

	var laterPending int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE id IN ('evt-refund-inventory','evt-refund-lifecycle') AND status='pending'`).Scan(&laterPending); err != nil {
		t.Fatal(err)
	}
	if laterPending != 2 {
		t.Fatalf("later same-order facts pending=%d want=2", laterPending)
	}
}
