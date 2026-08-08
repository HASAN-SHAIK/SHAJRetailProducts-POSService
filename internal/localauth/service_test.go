package localauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestEnrollLoginAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }

	secret := "unit-test-offline-grant-secret"
	service := New(db, secret)
	grant := signTestGrant(t, secret, map[string]any{
		"type": "pos_offline_grant",
		"user_id": "44",
		"tenant_id": "tenant-1",
		"role": "staff",
		"branch_id": "store-1",
		"all_branch_access": false,
		"permissions": []string{"products:read", "orders:read", "orders:write"},
		"grant_id": "grant-1",
		"iss": "shajtech-central",
		"aud": "shajtech-pos-edge",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	user, err := service.Enroll(ctx, grant, "2468")
	if err != nil { t.Fatalf("enroll: %v", err) }
	if user.UserID != "44" || user.BranchID != "store-1" { t.Fatalf("unexpected user: %+v", user) }

	if _, _, err := service.Login(ctx, "44", "1111"); err != ErrInvalidPIN {
		t.Fatalf("wrong pin error = %v, want %v", err, ErrInvalidPIN)
	}

	token, loggedIn, err := service.Login(ctx, "44", "2468")
	if err != nil { t.Fatalf("login: %v", err) }
	if token == "" || loggedIn.UserID != "44" { t.Fatalf("unexpected login result: token=%q user=%+v", token, loggedIn) }

	authenticated, err := service.Authenticate(ctx, token)
	if err != nil { t.Fatalf("authenticate: %v", err) }
	if authenticated.UserID != "44" || len(authenticated.Permissions) != 3 { t.Fatalf("unexpected authenticated user: %+v", authenticated) }
}

func signTestGrant(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "HS256", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(payload)
	input := h + "." + p
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
