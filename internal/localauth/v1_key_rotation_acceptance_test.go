package localauth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestV1KeyRotationAndGrantValidationAcceptance(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "rotation_test.db"))
	if err != nil {
		t.Fatalf("database open failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("database migration failed: %v", err)
	}

	// 1. Generate active (rotated v2) and previous (v1) keypairs
	v1Priv, v1Pub := testKeys(t)
	v2Priv, v2Pub := testKeys(t)
	attackerPriv, _ := testKeys(t)

	// Configure POSService with both v2 and v1 public keys during migration
	multiKeyPEM := v2Pub + "\n" + v1Pub
	service := New(db, multiKeyPEM)

	// 2. Validate grant signed with active rotated key (v2)
	validV2Grant := signTestGrant(t, v2Priv, map[string]any{
		"type":              "pos_offline_grant",
		"user_id":           "cashier-101",
		"tenant_id":         "tenant-alpha",
		"role":              "cashier",
		"device_id":         "term-01",
		"branch_id":         "branch-main",
		"all_branch_access": false,
		"permissions":       []string{"pos:billing", "pos:orders:read", "pos:orders:create"},
		"grant_id":          "grant-v2-active",
		"iss":               "shajtech-central",
		"aud":               "shajtech-pos-edge",
		"exp":               time.Now().Add(7 * 24 * time.Hour).Unix(),
	})

	userV2, err := service.EnrollForDevice(ctx, validV2Grant, "4321", "term-01", "branch-main")
	if err != nil {
		t.Fatalf("failed to enroll grant signed with rotated v2 key: %v", err)
	}
	if userV2.UserID != "cashier-101" || userV2.Role != "cashier" {
		t.Fatalf("unexpected enrolled user: %+v", userV2)
	}

	// 3. Validate grant signed with legacy key (v1) during migration window
	validV1Grant := signTestGrant(t, v1Priv, map[string]any{
		"type":              "pos_offline_grant",
		"user_id":           "cashier-102",
		"tenant_id":         "tenant-alpha",
		"role":              "staff",
		"device_id":         "term-01",
		"branch_id":         "branch-main",
		"all_branch_access": false,
		"permissions":       []string{"pos:billing"},
		"grant_id":          "grant-v1-legacy",
		"iss":               "shajtech-central",
		"aud":               "shajtech-pos-edge",
		"exp":               time.Now().Add(24 * time.Hour).Unix(),
	})

	userV1, err := service.EnrollForDevice(ctx, validV1Grant, "8765", "term-01", "branch-main")
	if err != nil {
		t.Fatalf("failed to enroll grant signed with legacy v1 key: %v", err)
	}
	if userV1.UserID != "cashier-102" {
		t.Fatalf("unexpected legacy user: %+v", userV1)
	}

	// 4. Test offline login operates independently without central connectivity
	token, sessionUser, err := service.Login(ctx, "cashier-101", "4321")
	if err != nil {
		t.Fatalf("offline login failed: %v", err)
	}
	if token == "" || sessionUser.UserID != "cashier-101" {
		t.Fatalf("invalid offline session: %+v", sessionUser)
	}

	// Session token authentication works
	authResult, err := service.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("local session authenticate failed: %v", err)
	}
	if authResult.UserID != "cashier-101" {
		t.Fatalf("unexpected authenticated user: %+v", authResult)
	}

	// 5. Test expired grant rejection
	expiredGrant := signTestGrant(t, v2Priv, map[string]any{
		"type":              "pos_offline_grant",
		"user_id":           "cashier-expired",
		"tenant_id":         "tenant-alpha",
		"role":              "cashier",
		"device_id":         "term-01",
		"branch_id":         "branch-main",
		"all_branch_access": false,
		"permissions":       []string{"pos:billing"},
		"grant_id":          "grant-expired",
		"iss":               "shajtech-central",
		"aud":               "shajtech-pos-edge",
		"exp":               time.Now().Add(-1 * time.Hour).Unix(), // Expired 1 hour ago
	})
	if _, err := service.EnrollForDevice(ctx, expiredGrant, "1111", "term-01", "branch-main"); err != ErrInvalidGrant {
		t.Fatalf("expected ErrInvalidGrant for expired grant, got: %v", err)
	}

	// 6. Test forged / incorrectly signed grant rejection (attacker key)
	forgedGrant := signTestGrant(t, attackerPriv, map[string]any{
		"type":              "pos_offline_grant",
		"user_id":           "cashier-attacker",
		"tenant_id":         "tenant-alpha",
		"role":              "admin",
		"device_id":         "term-01",
		"branch_id":         "branch-main",
		"all_branch_access": true,
		"permissions":       []string{"*"},
		"grant_id":          "grant-forged",
		"iss":               "shajtech-central",
		"aud":               "shajtech-pos-edge",
		"exp":               time.Now().Add(24 * time.Hour).Unix(),
	})
	if _, err := service.EnrollForDevice(ctx, forgedGrant, "1111", "term-01", "branch-main"); err != ErrInvalidGrant {
		t.Fatalf("expected ErrInvalidGrant for forged grant, got: %v", err)
	}

	// 7. Test tampered signature rejection
	tamperedSignatureGrant := validV2Grant[:len(validV2Grant)-6] + "xxxxxx"
	if _, err := service.EnrollForDevice(ctx, tamperedSignatureGrant, "1111", "term-01", "branch-main"); err != ErrInvalidGrant {
		t.Fatalf("expected ErrInvalidGrant for tampered signature, got: %v", err)
	}

	// 8. Test invalid claims rejection (wrong type)
	wrongTypeGrant := signTestGrant(t, v2Priv, map[string]any{
		"type":      "regular_access_token",
		"user_id":   "cashier-wrong-type",
		"tenant_id": "tenant-alpha",
		"role":      "cashier",
		"device_id": "term-01",
		"grant_id":  "grant-wrong-type",
		"iss":       "shajtech-central",
		"aud":       "shajtech-pos-edge",
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
	})
	if _, err := service.EnrollForDevice(ctx, wrongTypeGrant, "1111", "term-01", "branch-main"); err != ErrInvalidGrant {
		t.Fatalf("expected ErrInvalidGrant for wrong type claim, got: %v", err)
	}

	// 9. Test wrong issuer rejection
	wrongIssuerGrant := signTestGrant(t, v2Priv, map[string]any{
		"type":      "pos_offline_grant",
		"user_id":   "cashier-wrong-iss",
		"tenant_id": "tenant-alpha",
		"role":      "cashier",
		"device_id": "term-01",
		"grant_id":  "grant-wrong-iss",
		"iss":       "rogue-issuer",
		"aud":       "shajtech-pos-edge",
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
	})
	if _, err := service.EnrollForDevice(ctx, wrongIssuerGrant, "1111", "term-01", "branch-main"); err != ErrInvalidGrant {
		t.Fatalf("expected ErrInvalidGrant for wrong issuer, got: %v", err)
	}

	// 10. Test wrong audience rejection
	wrongAudienceGrant := signTestGrant(t, v2Priv, map[string]any{
		"type":      "pos_offline_grant",
		"user_id":   "cashier-wrong-aud",
		"tenant_id": "tenant-alpha",
		"role":      "cashier",
		"device_id": "term-01",
		"grant_id":  "grant-wrong-aud",
		"iss":       "shajtech-central",
		"aud":       "cloud-backend",
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
	})
	if _, err := service.EnrollForDevice(ctx, wrongAudienceGrant, "1111", "term-01", "branch-main"); err != ErrInvalidGrant {
		t.Fatalf("expected ErrInvalidGrant for wrong audience, got: %v", err)
	}
}
