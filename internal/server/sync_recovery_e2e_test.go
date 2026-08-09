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
	"sync"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/syncengine"
)

func TestCentralAuthorizedDeadLetterRecoveryResumesOrderedRefundSync(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos-recovery-e2e.db"))
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
	identity, err = deviceService.ApplyRegistration(ctx, device.Registration{StoreID: "store-e2e", TerminalID: "terminal-e2e"})
	if err != nil {
		t.Fatal(err)
	}

	orderID := "ord-sync-recovery-e2e"
	orderingKey := "sales_order:" + orderID
	now := time.Now().UTC()
	events := []struct {
		id            string
		aggregateType string
		aggregateID   string
		version       int
		eventType     string
		status        string
		attempts      int
	}{
		{"evt-recovery-payment", "payment", "pay-recovery-e2e", 2, "payment.recorded", "dead_letter", 12},
		{"evt-recovery-inventory", "inventory_movement", "mov-recovery-e2e", 1, "inventory.movement.recorded", "pending", 0},
		{"evt-recovery-partial", "sales_order", orderID, 3, "sale.partial_returned", "pending", 0},
	}
	for i, event := range events {
		createdAt := now.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)
		payload, _ := json.Marshal(map[string]any{"order_id": orderID, "sequence": i + 1})
		metadata, _ := json.Marshal(map[string]any{"source": "pos_service", "occurred_at": createdAt})
		if _, err := db.SQL().ExecContext(ctx, `
			INSERT INTO outbox_events(
				id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
				ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at,last_error
			) VALUES(?,?,?,?,?,1,?,?,?,?,?,?,?,?)`,
			event.id, event.aggregateType, event.aggregateID, event.version, event.eventType,
			orderingKey, string(payload), string(metadata), event.status, event.attempts, createdAt, createdAt, "poisoned"); err != nil {
			t.Fatal(err)
		}
	}

	privateKey, publicKeyPEM := recoveryE2EKeys(t)
	grant := recoveryE2ESignGrant(t, privateKey, map[string]any{
		"type":                "pos_sync_recovery_grant",
		"recovery_id":         "recovery-e2e-1",
		"tenant_id":           "tenant-e2e",
		"device_id":           identity.DeviceID,
		"order_id":            orderID,
		"ordering_key":        orderingKey,
		"event_id":            events[0].id,
		"approved_by_user_id": "manager-central-1",
		"reason":              "reviewed poisoned refund fact",
		"iss":                 "shajtech-central",
		"aud":                 "shajtech-pos-edge",
		"exp":                 time.Now().Add(10 * time.Minute).Unix(),
	})

	s := &Server{
		cfg: config.Config{
			CentralTenantID:    "tenant-e2e",
			OfflineGrantSecret: publicKeyPEM,
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

	var recoveredStatus string
	var recoveredAttempts int
	if err := db.SQL().QueryRowContext(ctx, `SELECT status,attempt_count FROM outbox_events WHERE id=?`, events[0].id).Scan(&recoveredStatus, &recoveredAttempts); err != nil {
		t.Fatal(err)
	}
	if recoveredStatus != "pending" || recoveredAttempts != 0 {
		t.Fatalf("recovered head status=%s attempts=%d", recoveredStatus, recoveredAttempts)
	}

	var mu sync.Mutex
	requests := []string{}
	central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sync/events" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		requests = append(requests, r.Header.Get("Idempotency-Key"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer central.Close()

	engine, err := syncengine.New(outbox.New(db), central.URL, "tenant-e2e", "sync-secret", identity.DeviceID, 2*time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go engine.Run(runCtx)

	deadline := time.Now().Add(3 * time.Second)
	for {
		var published int
		if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE ordering_key=? AND status='published'`, orderingKey).Scan(&published); err != nil {
			t.Fatal(err)
		}
		if published == len(events) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refund recovery sync did not converge before deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	mu.Lock()
	got := append([]string(nil), requests...)
	mu.Unlock()
	want := []string{events[0].id, events[1].id, events[2].id}
	if len(got) != len(want) {
		t.Fatalf("Central requests=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Central request[%d]=%s want=%s all=%v", i, got[i], want[i], got)
		}
	}
}

func recoveryE2EKeys(t *testing.T) (*rsa.PrivateKey, string) {
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

func recoveryE2ESignGrant(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any) string {
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
