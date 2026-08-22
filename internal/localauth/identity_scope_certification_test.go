package localauth

import "testing"

func TestIdentityEnrollmentFailsClosedAcrossTenantStoreAndDevice(t *testing.T) {
	ctx, service, key, db := openV1AuthDB(t)
	defer db.Close()

	grant := v1Grant(t, key, map[string]any{
		"user_id":    "staff-scope-1",
		"tenant_id":  "tenant-a",
		"device_id":  "device-a",
		"branch_id":  "store-a",
		"grant_id":   "grant-scope-1",
		"permissions": []string{"pos:sale"},
	})

	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-a", "store-a", "tenant-b"); err != ErrInvalidGrant {
		t.Fatalf("cross-tenant grant accepted: %v", err)
	}
	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-a", "store-b", "tenant-a"); err != ErrInvalidGrant {
		t.Fatalf("cross-store grant accepted: %v", err)
	}
	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-b", "store-a", "tenant-a"); err != ErrInvalidGrant {
		t.Fatalf("cross-device grant accepted: %v", err)
	}

	var count int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM local_users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected scope mutations persisted %d local users", count)
	}

	user, err := service.EnrollForDevice(ctx, grant, "2468", "device-a", "store-a", "tenant-a")
	if err != nil {
		t.Fatalf("matching tenant/store/device grant rejected: %v", err)
	}
	if user.UserID != "staff-scope-1" || user.TenantID != "tenant-a" || user.BranchID != "store-a" {
		t.Fatalf("unexpected enrolled identity: %#v", user)
	}
}

func TestAllBranchIdentityStillRequiresTenantAndDeviceBinding(t *testing.T) {
	ctx, service, key, db := openV1AuthDB(t)
	defer db.Close()

	grant := v1Grant(t, key, map[string]any{
		"user_id":           "manager-all-branches",
		"tenant_id":         "tenant-a",
		"device_id":         "device-a",
		"branch_id":         "store-home",
		"all_branch_access": true,
		"grant_id":          "grant-all-branches",
		"permissions":       []string{"pos:sale", "pos:approve"},
	})

	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-a", "store-b", "tenant-a"); err != nil {
		t.Fatalf("all-branch identity should be allowed at another store in same tenant/device: %v", err)
	}

	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-a", "store-b", "tenant-b"); err != ErrInvalidGrant {
		t.Fatalf("all-branch identity crossed tenant boundary: %v", err)
	}
	if _, err := service.EnrollForDevice(ctx, grant, "2468", "device-b", "store-b", "tenant-a"); err != ErrInvalidGrant {
		t.Fatalf("all-branch identity crossed device boundary: %v", err)
	}
}
