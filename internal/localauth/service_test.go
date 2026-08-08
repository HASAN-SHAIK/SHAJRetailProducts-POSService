package localauth

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
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func testKeys(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil { t.Fatal(err) }
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil { t.Fatal(err) }
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
}

func TestEnrollLoginAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }

	privateKey, publicPEM := testKeys(t)
	service := New(db, publicPEM)
	grant := signTestGrant(t, privateKey, map[string]any{
		"type": "pos_offline_grant", "user_id": "44", "tenant_id": "tenant-1", "role": "staff",
		"device_id": "device-1", "branch_id": "store-1", "all_branch_access": false,
		"permissions": []string{"products:read", "orders:read", "orders:write"},
		"grant_id": "grant-1", "iss": "shajtech-central", "aud": "shajtech-pos-edge", "exp": time.Now().Add(time.Hour).Unix(),
	})

	user, err := service.EnrollForDevice(ctx, grant, "2468", "device-1", "store-1")
	if err != nil { t.Fatalf("enroll: %v", err) }
	if user.UserID != "44" || user.BranchID != "store-1" { t.Fatalf("unexpected user: %+v", user) }
	if _, _, err := service.Login(ctx, "44", "1111"); err != ErrInvalidPIN { t.Fatalf("wrong pin error = %v", err) }
	token, loggedIn, err := service.Login(ctx, "44", "2468")
	if err != nil { t.Fatalf("login: %v", err) }
	if token == "" || loggedIn.UserID != "44" { t.Fatalf("unexpected login result") }
	authenticated, err := service.Authenticate(ctx, token)
	if err != nil { t.Fatalf("authenticate: %v", err) }
	if authenticated.UserID != "44" || len(authenticated.Permissions) != 3 { t.Fatalf("unexpected authenticated user: %+v", authenticated) }
}

func TestRejectsGrantForDifferentDeviceOrStore(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }
	privateKey, publicPEM := testKeys(t)
	service := New(db, publicPEM)
	grant := signTestGrant(t, privateKey, map[string]any{
		"type": "pos_offline_grant", "user_id": "44", "tenant_id": "tenant-1", "role": "staff",
		"device_id": "device-1", "branch_id": "store-1", "all_branch_access": false,
		"permissions": []string{"orders:write"}, "grant_id": "grant-1",
		"iss": "shajtech-central", "aud": "shajtech-pos-edge", "exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-2", "store-1"); err != ErrInvalidGrant { t.Fatalf("device mismatch error = %v", err) }
	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-1", "store-2"); err != ErrInvalidGrant { t.Fatalf("store mismatch error = %v", err) }
}

func TestRejectsGrantSignedByUnknownPrivateKey(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }
	trusted, publicPEM := testKeys(t)
	_ = trusted
	attacker, _ := rsa.GenerateKey(rand.Reader, 2048)
	service := New(db, publicPEM)
	grant := signTestGrant(t, attacker, map[string]any{
		"type": "pos_offline_grant", "user_id": "44", "tenant_id": "tenant-1", "role": "staff", "device_id": "device-1",
		"grant_id": "forged", "iss": "shajtech-central", "aud": "shajtech-pos-edge", "exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-1", "store-1"); err != ErrInvalidGrant { t.Fatalf("forged grant error = %v", err) }
}

func signTestGrant(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "pos-offline-v1"})
	payload, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(payload)
	input := h + "." + p
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil { t.Fatal(err) }
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}
