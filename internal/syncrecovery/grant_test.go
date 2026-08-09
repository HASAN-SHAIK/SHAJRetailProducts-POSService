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

func testRecoveryKeys(t *testing.T) (*rsa.PrivateKey, string) {
    t.Helper()
    privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil { t.Fatal(err) }
    publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
    if err != nil { t.Fatal(err) }
    return privateKey, string(pem.EncodeToMemory(&pem.Block{Type:"PUBLIC KEY",Bytes:publicDER}))
}

func signRecoveryGrant(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any) string {
    t.Helper()
    header, _ := json.Marshal(map[string]any{"alg":"RS256","typ":"JWT","kid":"pos-offline-v1"})
    payload, _ := json.Marshal(claims)
    h := base64.RawURLEncoding.EncodeToString(header)
    p := base64.RawURLEncoding.EncodeToString(payload)
    input := h + "." + p
    digest := sha256.Sum256([]byte(input))
    signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
    if err != nil { t.Fatal(err) }
    return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func validRecoveryClaims() map[string]any {
    return map[string]any{
        "type":"pos_sync_recovery_grant","recovery_id":"recovery-1","tenant_id":"tenant-1",
        "device_id":"device-1","order_id":"ord-1","ordering_key":"sales_order:ord-1",
        "event_id":"evt-1","approved_by_user_id":"manager-1","reason":"reviewed dead letter",
        "iss":"shajtech-central","aud":"shajtech-pos-edge","exp":time.Now().Add(10*time.Minute).Unix(),
    }
}

func TestVerifyCentralRecoveryGrant(t *testing.T) {
    privateKey, publicPEM := testRecoveryKeys(t)
    grant, err := Verify(signRecoveryGrant(t, privateKey, validRecoveryClaims()), publicPEM)
    if err != nil { t.Fatal(err) }
    if grant.EventID != "evt-1" || grant.OrderingKey != "sales_order:ord-1" || grant.ApprovedByUserID != "manager-1" { t.Fatalf("unexpected grant: %+v", grant) }
}

func TestRejectsForgedExpiredOrMismatchedRecoveryGrant(t *testing.T) {
    trusted, publicPEM := testRecoveryKeys(t)
    attacker, _ := rsa.GenerateKey(rand.Reader, 2048)
    if _, err := Verify(signRecoveryGrant(t, attacker, validRecoveryClaims()), publicPEM); err != ErrInvalidGrant { t.Fatalf("forged grant error=%v", err) }

    expired := validRecoveryClaims(); expired["exp"] = time.Now().Add(-time.Minute).Unix()
    if _, err := Verify(signRecoveryGrant(t, trusted, expired), publicPEM); err != ErrInvalidGrant { t.Fatalf("expired grant error=%v", err) }

    mismatched := validRecoveryClaims(); mismatched["ordering_key"] = "sales_order:other"
    if _, err := Verify(signRecoveryGrant(t, trusted, mismatched), publicPEM); err != ErrInvalidGrant { t.Fatalf("mismatched ordering key error=%v", err) }
}
