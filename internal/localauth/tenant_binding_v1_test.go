package localauth

import (
	"testing"
)

func TestV1OfflineGrantEnrollmentRequiresConfiguredTenant(t *testing.T) {
	ctx, service, key, db := openV1AuthDB(t)
	defer db.Close()

	grant := v1Grant(t, key, map[string]any{
		"tenant_id": "tenant-b",
		"device_id": "shared-device",
		"branch_id": "store-1",
		"grant_id": "grant-tenant-b",
	})

	if _, err := service.EnrollForDevice(ctx, grant, "2468", "shared-device", "store-1", "tenant-a"); err != ErrInvalidGrant {
		t.Fatalf("cross-tenant grant was accepted: %v", err)
	}

	var count int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM local_users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cross-tenant enrollment persisted %d local users", count)
	}

	user, err := service.EnrollForDevice(ctx, grant, "2468", "shared-device", "store-1", "tenant-b")
	if err != nil {
		t.Fatalf("matching tenant grant rejected: %v", err)
	}
	if user.TenantID != "tenant-b" {
		t.Fatalf("unexpected enrolled tenant: %s", user.TenantID)
	}
}
