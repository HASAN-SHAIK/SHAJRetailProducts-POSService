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

func recoveryCertificationKeys(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
}

func signRecoveryCertificationGrant(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any) string {
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

func insertRecoveryOrderingFact(t *testing.T, db *database.DB, id, orderID string, version int, eventType, createdAt string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
			ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
		) VALUES(?,?,?,?,?,1,?,'{}','{}','pending',0,?,?)`,
		id, "sales_order", orderID, version, eventType, "sales_order:"+orderID, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
}

func TestCentralAuthorizedRecoveryRouteResumesRefundFactsInOrder(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "sync-recovery-ordering.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	deviceService := device.New(db)
	identity, err := deviceService.EnsureInstallation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	const orderID = "ord-recovery-cert"
	const headID = "evt-recovery-payment"
	base := time.Now().UTC().Add(-time.Minute)
	insertRecoveryOrderingFact(t, db, headID, orderID, 1, "payment.recorded", base.Format(time.RFC3339Nano))
	insertRecoveryOrderingFact(t, db, "evt-recovery-inventory", orderID, 2, "inventory.movement.recorded", base.Add(time.Millisecond).Format(time.RFC3339Nano))
	insertRecoveryOrderingFact(t, db, "evt-recovery-partial", orderID, 3, "sale.partial_returned", base.Add(2*time.Millisecond).Format(time.RFC3339Nano))

	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET status='dead_letter',attempt_count=12 WHERE id=?`, headID); err != nil {
		t.Fatal(err)
	}

	outboxService := outbox.New(db)
	blocked, err := outboxService.ClaimNext(ctx, "worker-before-recovery")
	if err != nil {
		t.Fatal(err)
	}
	if blocked != nil {
		t.Fatalf("same-order fact overtook dead-letter head before recovery: %+v", blocked)
	}

	privateKey, publicPEM := recoveryCertificationKeys(t)
	grant := signRecoveryCertificationGrant(t, privateKey, map[string]any{
		"type":                "pos_sync_recovery_grant",
		"recovery_id":         "recovery-cert-1",
		"tenant_id":           "tenant-cert",
		"device_id":           identity.DeviceID,
		"order_id":            orderID,
		"ordering_key":        "sales_order:" + orderID,
		"event_id":            headID,
		"approved_by_user_id": "manager-central-1",
		"reason":              "reviewed refund dead letter",
		"iss":                 "shajtech-central",
		"aud":                 "shajtech-pos-edge",
		"exp":                 time.Now().Add(10 * time.Minute).Unix(),
	})

	s := &Server{
		cfg: config.Config{
			CentralTenantID:    "tenant-cert",
			OfflineGrantSecret: publicPEM,
		},
		db:     db,
		device: deviceService,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID+"/sync-recovery", strings.NewReader(`{"grant":"`+grant+`"}`))
	req.SetPathValue("id", orderID)
	res := httptest.NewRecorder()
	s.handleSyncRecovery(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("recovery status=%d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"recovery_id":"recovery-cert-1"`) || !strings.Contains(res.Body.String(), `"status":"pending"`) {
		t.Fatalf("recovery response lost audit identity or pending transition: %s", res.Body.String())
	}

	var status string
	var attempts int
	if err := db.SQL().QueryRowContext(ctx, `SELECT status,attempt_count FROM outbox_events WHERE id=?`, headID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 0 {
		t.Fatalf("authorized recovery status=%s attempts=%d want pending/0", status, attempts)
	}

	// Recovery does not skip the poisoned refund head. The normal outbox claim
	// path must retry that exact fact first, then allow inventory and lifecycle
	// facts for the same order only after the predecessor is published.
	head, err := outboxService.ClaimNext(ctx, "worker-after-recovery")
	if err != nil {
		t.Fatal(err)
	}
	if head == nil || head.ID != headID || head.EventType != "payment.recorded" {
		t.Fatalf("first post-recovery claim=%+v want payment head", head)
	}
	if err := outboxService.MarkPublished(ctx, head.ID, "worker-after-recovery"); err != nil {
		t.Fatal(err)
	}

	inventoryFact, err := outboxService.ClaimNext(ctx, "worker-after-recovery")
	if err != nil {
		t.Fatal(err)
	}
	if inventoryFact == nil || inventoryFact.ID != "evt-recovery-inventory" || inventoryFact.EventType != "inventory.movement.recorded" {
		t.Fatalf("second post-recovery claim=%+v want inventory fact", inventoryFact)
	}
	if err := outboxService.MarkPublished(ctx, inventoryFact.ID, "worker-after-recovery"); err != nil {
		t.Fatal(err)
	}

	partialFact, err := outboxService.ClaimNext(ctx, "worker-after-recovery")
	if err != nil {
		t.Fatal(err)
	}
	if partialFact == nil || partialFact.ID != "evt-recovery-partial" || partialFact.EventType != "sale.partial_returned" {
		t.Fatalf("third post-recovery claim=%+v want partial-return fact", partialFact)
	}
}
