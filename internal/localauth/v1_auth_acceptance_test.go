package localauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func openV1AuthDB(t *testing.T) (context.Context, *Service, *rsa.PrivateKey, *database.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	if err := db.Migrate(ctx); err != nil { db.Close(); t.Fatal(err) }
	privateKey, publicPEM := testKeys(t)
	return ctx, New(db, publicPEM), privateKey, db
}

func v1Grant(t *testing.T, key *rsa.PrivateKey, overrides map[string]any) string {
	t.Helper()
	claims := map[string]any{
		"type": "pos_offline_grant",
		"user_id": "44",
		"tenant_id": "tenant-1",
		"role": "cashier",
		"device_id": "device-1",
		"branch_id": "store-1",
		"all_branch_access": false,
		"permissions": []string{"products:read", "orders:read", "pos:sale"},
		"grant_id": "grant-1",
		"iss": "shajtech-central",
		"aud": "shajtech-pos-edge",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range overrides { claims[k] = v }
	return signTestGrant(t, key, claims)
}

func TestV1OfflineGrantClaimsFailClosed(t *testing.T) {
	ctx, service, key, db := openV1AuthDB(t)
	defer db.Close()

	cases := []struct {
		name string
		overrides map[string]any
	}{
		{"expired", map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}},
		{"wrong issuer", map[string]any{"iss": "attacker"}},
		{"wrong audience", map[string]any{"aud": "other-edge"}},
		{"wrong type", map[string]any{"type": "tenant"}},
		{"missing grant id", map[string]any{"grant_id": ""}},
		{"missing user", map[string]any{"user_id": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grant := v1Grant(t, key, tc.overrides)
			if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-1", "store-1"); err != ErrInvalidGrant {
				t.Fatalf("expected invalid grant, got %v", err)
			}
		})
	}
}

func TestV1PINPolicyAndStorage(t *testing.T) {
	ctx, service, key, db := openV1AuthDB(t)
	defer db.Close()
	grant := v1Grant(t, key, nil)

	for _, pin := range []string{"123", "123456789", "12a4", ""} {
		if _, err := service.EnrollForDevice(ctx, grant, pin, "device-1", "store-1"); err != ErrInvalidPIN {
			t.Fatalf("pin %q error=%v", pin, err)
		}
	}
	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-1", "store-1"); err != nil { t.Fatal(err) }

	var salt, hash []byte
	var iterations int
	if err := db.SQL().QueryRowContext(ctx, `SELECT pin_salt, pin_hash, pin_iterations FROM local_users WHERE user_id='44'`).Scan(&salt, &hash, &iterations); err != nil { t.Fatal(err) }
	if string(hash) == "2468" || string(salt) == "2468" { t.Fatal("plaintext PIN persisted") }
	if len(salt) != 16 || len(hash) != pinKeyLen || iterations != pinIterations {
		t.Fatalf("unexpected PIN derivation metadata salt=%d hash=%d iterations=%d", len(salt), len(hash), iterations)
	}
}

func TestV1LockoutLogoutAndReenrollmentInvalidateSessions(t *testing.T) {
	ctx, service, key, db := openV1AuthDB(t)
	defer db.Close()
	grant := v1Grant(t, key, nil)
	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-1", "store-1"); err != nil { t.Fatal(err) }

	for i := 0; i < maxFailedAttempts; i++ {
		if _, _, err := service.Login(ctx, "44", "1111"); err != ErrInvalidPIN { t.Fatalf("attempt %d err=%v", i+1, err) }
	}
	if _, _, err := service.Login(ctx, "44", "2468"); err != ErrLocked { t.Fatalf("expected lockout, got %v", err) }

	// Clear only the test lockout so we can certify session lifecycle separately.
	if _, err := db.SQL().ExecContext(ctx, `UPDATE local_users SET locked_until=NULL, failed_attempts=0 WHERE user_id='44'`); err != nil { t.Fatal(err) }
	token, _, err := service.Login(ctx, "44", "2468")
	if err != nil { t.Fatal(err) }
	if _, err := service.Authenticate(ctx, token); err != nil { t.Fatalf("authenticate: %v", err) }
	service.Logout(ctx, token)
	if _, err := service.Authenticate(ctx, token); err != ErrSessionInvalid { t.Fatalf("logout session err=%v", err) }

	oldToken, _, err := service.Login(ctx, "44", "2468")
	if err != nil { t.Fatal(err) }
	newGrant := v1Grant(t, key, map[string]any{"grant_id": "grant-2", "permissions": []string{"products:read"}})
	if _, err := service.EnrollForDevice(ctx, newGrant, "1357", "device-1", "store-1"); err != nil { t.Fatal(err) }
	if _, err := service.Authenticate(ctx, oldToken); err != ErrSessionInvalid { t.Fatalf("old session survived re-enrollment: %v", err) }
	if _, _, err := service.Login(ctx, "44", "2468"); err != ErrInvalidPIN { t.Fatalf("old PIN still valid: %v", err) }
	if _, user, err := service.Login(ctx, "44", "1357"); err != nil || user.GrantID != "grant-2" || len(user.Permissions) != 1 {
		t.Fatalf("new enrollment not active user=%+v err=%v", user, err)
	}
}

func TestV1GrantSignedByUnknownKeyStillFails(t *testing.T) {
	ctx, service, _, db := openV1AuthDB(t)
	defer db.Close()
	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil { t.Fatal(err) }
	grant := v1Grant(t, attacker, nil)
	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-1", "store-1"); err != ErrInvalidGrant {
		t.Fatalf("forged grant err=%v", err)
	}
}
