package config

import "testing"

func TestLoadUsesCanonicalEnvironmentNames(t *testing.T) {
	t.Setenv("POS_SERVICE_ADDRESS", "")
	t.Setenv("POS_DATABASE_PATH", "")
	t.Setenv("POS_CENTRAL_TENANT_ID", "")
	t.Setenv("POS_CENTRAL_SYNC_TOKEN", "")
	t.Setenv("POS_SYNC_POLL_INTERVAL", "")
	t.Setenv("POS_LISTEN_ADDRESS", "127.0.0.1:4790")
	t.Setenv("POS_SQLITE_PATH", "./data/test-pos.db")
	t.Setenv("POS_CENTRAL_API_URL", "http://localhost:5001/")
	t.Setenv("POS_SYNC_TENANT_ID", "tenant-dev")
	t.Setenv("POS_SYNC_TOKEN", "sync-secret")
	t.Setenv("POS_SYNC_INTERVAL", "3s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.ListenAddress != "127.0.0.1:4790" {
		t.Fatalf("ListenAddress = %q", cfg.ListenAddress)
	}
	if cfg.DatabasePath != "./data/test-pos.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.CentralAPIURL != "http://localhost:5001" {
		t.Fatalf("CentralAPIURL = %q", cfg.CentralAPIURL)
	}
	if cfg.CentralTenantID != "tenant-dev" {
		t.Fatalf("CentralTenantID = %q", cfg.CentralTenantID)
	}
	if cfg.CentralSyncToken != "sync-secret" {
		t.Fatalf("CentralSyncToken = %q", cfg.CentralSyncToken)
	}
	if cfg.SyncPollInterval.String() != "3s" {
		t.Fatalf("SyncPollInterval = %s", cfg.SyncPollInterval)
	}
}

func TestLoadSupportsLegacyEnvironmentAliases(t *testing.T) {
	t.Setenv("POS_LISTEN_ADDRESS", "")
	t.Setenv("POS_SQLITE_PATH", "")
	t.Setenv("POS_SYNC_TENANT_ID", "")
	t.Setenv("POS_SYNC_TOKEN", "")
	t.Setenv("POS_SYNC_INTERVAL", "")
	t.Setenv("POS_SERVICE_ADDRESS", "127.0.0.1:4791")
	t.Setenv("POS_DATABASE_PATH", "./data/legacy-pos.db")
	t.Setenv("POS_CENTRAL_API_URL", "http://localhost:5001")
	t.Setenv("POS_CENTRAL_TENANT_ID", "tenant-legacy")
	t.Setenv("POS_CENTRAL_SYNC_TOKEN", "legacy-secret")
	t.Setenv("POS_SYNC_POLL_INTERVAL", "4s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.ListenAddress != "127.0.0.1:4791" {
		t.Fatalf("ListenAddress = %q", cfg.ListenAddress)
	}
	if cfg.DatabasePath != "./data/legacy-pos.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.CentralTenantID != "tenant-legacy" {
		t.Fatalf("CentralTenantID = %q", cfg.CentralTenantID)
	}
	if cfg.CentralSyncToken != "legacy-secret" {
		t.Fatalf("CentralSyncToken = %q", cfg.CentralSyncToken)
	}
	if cfg.SyncPollInterval.String() != "4s" {
		t.Fatalf("SyncPollInterval = %s", cfg.SyncPollInterval)
	}
}

func TestLoadRequiresSyncCredentialsWhenCentralAPIConfigured(t *testing.T) {
	t.Setenv("POS_SYNC_TENANT_ID", "")
	t.Setenv("POS_SYNC_TOKEN", "")
	t.Setenv("POS_CENTRAL_TENANT_ID", "")
	t.Setenv("POS_CENTRAL_SYNC_TOKEN", "")
	t.Setenv("POS_CENTRAL_API_URL", "http://localhost:5001")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error")
	}
}

func TestLoadWrapsBase64OfflineGrantPublicKey(t *testing.T) {
	t.Setenv("POS_LISTEN_ADDRESS", "127.0.0.1:4792")
	t.Setenv("POS_SQLITE_PATH", "./data/test-pos.db")
	t.Setenv("POS_CENTRAL_API_URL", "")
	t.Setenv("POS_OFFLINE_GRANT_PUBLIC_KEY", "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	want := "-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A\n-----END PUBLIC KEY-----"
	if cfg.OfflineGrantSecret != want {
		t.Fatalf("OfflineGrantSecret = %q", cfg.OfflineGrantSecret)
	}
}
