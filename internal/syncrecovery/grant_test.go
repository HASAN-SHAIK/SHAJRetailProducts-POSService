package syncrecovery

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"
)

func signedRecoveryGrant(t *testing.T, privateKey *rsa.PrivateKey, mutate func(map[string]any)) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": "test-key"}
	claims := map[string]any{
		"type": "pos_sync_recovery_grant",
		"recovery_id": "recovery-1",
		"tenant_id": "tenant-1",
		"device_id": "device-1",
		"order_id": "ord-1",
		"ordering_key": "sales_order:ord-1",
		"event_id": "evt-dead-1",
		"approved_by_user_id": "manager-1",
		"reason": "reviewed dead-letter and approved retry",
		"exp": time.Now().UTC().Add(5 * time.Minute).Unix(),
		"iss": "shajtech-central",
		"aud": "shajtech-pos-edge",
	}
	if mutate != nil {
		mutate(claims)
	}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func recoveryKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
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

func TestVerifyCentralRecoveryGrant(t *testing.T) {
	privateKey, publicKey := recoveryKeyPair(t)
	grant, err := Verify(signedRecoveryGrant(t, privateKey, nil), publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if grant.RecoveryID != "recovery-1" || grant.ApprovedByUserID != "manager-1" || grant.Reason == "" {
		t.Fatalf("unexpected grant: %+v", grant)
	}
	if !grant.Matches("tenant-1", "device-1", "ord-1", "evt-dead-1") {
		t.Fatal("expected exact recovery scope to match")
	}
	if grant.Matches("tenant-1", "device-2", "ord-1", "evt-dead-1") || grant.Matches("tenant-1", "device-1", "ord-2", "evt-dead-1") || grant.Matches("tenant-1", "device-1", "ord-1", "evt-other") {
		t.Fatal("recovery grant must not match a different device/order/event scope")
	}
}

func TestVerifyRejectsExpiredOrMalformedScope(t *testing.T) {
	privateKey, publicKey := recoveryKeyPair(t)
	cases := []struct {
		name string
		mutate func(map[string]any)
	}{
		{"expired", func(c map[string]any) { c["exp"] = time.Now().UTC().Add(-time.Minute).Unix() }},
		{"wrong type", func(c map[string]any) { c["type"] = "pos_offline_grant" }},
		{"wrong issuer", func(c map[string]any) { c["iss"] = "not-central" }},
		{"wrong audience", func(c map[string]any) { c["aud"] = "other" }},
		{"ordering mismatch", func(c map[string]any) { c["ordering_key"] = "sales_order:ord-2" }},
		{"missing reason", func(c map[string]any) { c["reason"] = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Verify(signedRecoveryGrant(t, privateKey, tc.mutate), publicKey); err != ErrInvalidGrant {
				t.Fatalf("expected ErrInvalidGrant, got %v", err)
			}
		})
	}
}

func TestVerifyRejectsTamperedGrant(t *testing.T) {
	privateKey, publicKey := recoveryKeyPair(t)
	token := signedRecoveryGrant(t, privateKey, nil)
	parts := []byte(token)
	parts[len(parts)-1] ^= 1
	if _, err := Verify(string(parts), publicKey); err != ErrInvalidGrant {
		t.Fatalf("expected tampered grant rejection, got %v", err)
	}
}
