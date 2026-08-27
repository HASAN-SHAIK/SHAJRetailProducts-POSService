package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment        string
	ListenAddress      string
	DatabasePath       string
	CentralAPIURL      string
	CentralTenantID    string
	CentralSyncToken   string
	DeviceID           string
	InstallationID     string
	StoreID            string
	TerminalID         string
	SyncRequestTimeout time.Duration
	SyncPollInterval   time.Duration
	LocalAPIToken      string
	LocalTokenFile     string
	// OfflineGrantSecret is retained as an internal field name for compatibility;
	// it now contains Central's RSA public verification key, never signing material.
	OfflineGrantSecret    string
	AllowedOrigins        []string
	BackupDirectory       string
	BackupInterval        time.Duration
	BackupRetention       int
	ObservabilityInterval time.Duration
}

func Load() (Config, error) {
	requestTimeout, err := durationEnv("POS_SYNC_REQUEST_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	pollInterval, err := durationEnvAlias("POS_SYNC_INTERVAL", "POS_SYNC_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	backupInterval, err := durationEnv("POS_BACKUP_INTERVAL", 6*time.Hour)
	if err != nil {
		return Config{}, err
	}
	observabilityInterval, err := durationEnv("POS_OBSERVABILITY_INTERVAL", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	retention, err := intEnv("POS_BACKUP_RETENTION", 14)
	if err != nil {
		return Config{}, err
	}

	databasePath := envOrDefaultAlias("POS_SQLITE_PATH", "POS_DATABASE_PATH", "./data/shajretail-pos.db")
	cfg := Config{
		Environment: envOrDefault("POS_ENVIRONMENT", "development"), ListenAddress: envOrDefaultAlias("POS_LISTEN_ADDRESS", "POS_SERVICE_ADDRESS", "127.0.0.1:4782"),
		DatabasePath: databasePath, CentralAPIURL: strings.TrimRight(strings.TrimSpace(os.Getenv("POS_CENTRAL_API_URL")), "/"),
		CentralTenantID: strings.TrimSpace(envAlias("POS_SYNC_TENANT_ID", "POS_CENTRAL_TENANT_ID")), CentralSyncToken: strings.TrimSpace(envAlias("POS_SYNC_TOKEN", "POS_CENTRAL_SYNC_TOKEN")),
		DeviceID: strings.TrimSpace(os.Getenv("POS_DEVICE_ID")), InstallationID: strings.TrimSpace(os.Getenv("POS_INSTALLATION_ID")),
		StoreID: strings.TrimSpace(os.Getenv("POS_STORE_ID")), TerminalID: strings.TrimSpace(os.Getenv("POS_TERMINAL_ID")),
		SyncRequestTimeout: requestTimeout, SyncPollInterval: pollInterval,
		LocalAPIToken: os.Getenv("POS_LOCAL_API_TOKEN"), LocalTokenFile: envOrDefault("POS_LOCAL_TOKEN_FILE", databasePath+".token"),
		OfflineGrantSecret: normalizeMultilineEnv("POS_OFFLINE_GRANT_PUBLIC_KEY"),
		AllowedOrigins:     csvEnv("POS_ALLOWED_ORIGINS", []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:5173", "http://127.0.0.1:5173"}),
		BackupDirectory:    envOrDefault("POS_BACKUP_DIRECTORY", databasePath+".backups"), BackupInterval: backupInterval, BackupRetention: retention,
		ObservabilityInterval: observabilityInterval,
	}
	if cfg.ListenAddress == "" {
		return Config{}, fmt.Errorf("POS_LISTEN_ADDRESS cannot be empty")
	}
	if cfg.DatabasePath == "" {
		return Config{}, fmt.Errorf("POS_SQLITE_PATH cannot be empty")
	}
	if cfg.LocalTokenFile == "" {
		return Config{}, fmt.Errorf("POS_LOCAL_TOKEN_FILE cannot be empty")
	}
	if cfg.BackupDirectory == "" {
		return Config{}, fmt.Errorf("POS_BACKUP_DIRECTORY cannot be empty")
	}
	if cfg.SyncRequestTimeout <= 0 || cfg.SyncPollInterval <= 0 || cfg.BackupInterval <= 0 || cfg.ObservabilityInterval <= 0 {
		return Config{}, fmt.Errorf("configured durations must be positive")
	}
	if cfg.BackupRetention <= 0 {
		return Config{}, fmt.Errorf("POS_BACKUP_RETENTION must be positive")
	}
	if cfg.CentralAPIURL != "" && (cfg.CentralTenantID == "" || cfg.CentralSyncToken == "") {
		return Config{}, fmt.Errorf("POS_SYNC_TENANT_ID and POS_SYNC_TOKEN are required when POS_CENTRAL_API_URL is configured")
	}
	if err := validateLoopbackAddress(cfg.ListenAddress); err != nil {
		return Config{}, err
	}
	if len(cfg.AllowedOrigins) == 0 {
		return Config{}, fmt.Errorf("POS_ALLOWED_ORIGINS must contain at least one origin")
	}
	if err := validateProductionSecurity(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateProductionSecurity(cfg Config) error {
	if !strings.EqualFold(strings.TrimSpace(cfg.Environment), "production") {
		return nil
	}
	if strings.TrimSpace(cfg.OfflineGrantSecret) == "" {
		return fmt.Errorf("POS_OFFLINE_GRANT_PUBLIC_KEY is required in production")
	}
	if strings.TrimSpace(cfg.DeviceID) != "" || strings.TrimSpace(cfg.InstallationID) != "" || strings.TrimSpace(cfg.StoreID) != "" || strings.TrimSpace(cfg.TerminalID) != "" {
		return fmt.Errorf("development POS identity overrides are not allowed in production")
	}
	if cfg.CentralAPIURL != "" && isPlaceholderSecret(cfg.CentralSyncToken) {
		return fmt.Errorf("POS_SYNC_TOKEN must not use a placeholder value in production")
	}
	if token := strings.TrimSpace(cfg.LocalAPIToken); token != "" && isPlaceholderSecret(token) {
		return fmt.Errorf("POS_LOCAL_API_TOKEN must not use a placeholder value in production")
	}
	return nil
}

func isPlaceholderSecret(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "change-me", "changeme", "change-me-in-development", "replace-me", "replace_me", "default", "secret":
		return true
	default:
		return false
	}
}

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("POS_LISTEN_ADDRESS: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("POS_LISTEN_ADDRESS must bind to loopback only")
	}
	return nil
}
func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envAlias(key, oldKey string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	if value := os.Getenv(oldKey); value != "" {
		warnDeprecatedEnv(oldKey, key)
		return value
	}
	return ""
}
func envOrDefaultAlias(key, oldKey, fallback string) string {
	if value := envAlias(key, oldKey); value != "" {
		return value
	}
	return fallback
}
func normalizeMultilineEnv(key string) string {
	value := strings.TrimSpace(strings.ReplaceAll(os.Getenv(key), `\n`, "\n"))
	if value == "" || strings.Contains(value, "-----BEGIN ") {
		return value
	}
	return "-----BEGIN PUBLIC KEY-----\n" + wrapPEMBody(value) + "\n-----END PUBLIC KEY-----"
}
func wrapPEMBody(value string) string {
	body := strings.Join(strings.Fields(value), "")
	if body == "" {
		return ""
	}
	var builder strings.Builder
	for i := 0; i < len(body); i += 64 {
		if i > 0 {
			builder.WriteByte('\n')
		}
		end := i + 64
		if end > len(body) {
			end = len(body)
		}
		builder.WriteString(body[i:end])
	}
	return builder.String()
}
func csvEnv(key string, fallback []string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return append([]string(nil), fallback...)
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimRight(strings.TrimSpace(part), "/"); value != "" {
			out = append(out, value)
		}
	}
	return out
}
func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}
func durationEnvAlias(key, oldKey string, fallback time.Duration) (time.Duration, error) {
	if os.Getenv(key) != "" {
		return durationEnv(key, fallback)
	}
	if os.Getenv(oldKey) != "" {
		warnDeprecatedEnv(oldKey, key)
		return durationEnv(oldKey, fallback)
	}
	return fallback, nil
}
func intEnv(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}
func warnDeprecatedEnv(oldKey, newKey string) {
	slog.Warn("deprecated environment variable configured", "old", oldKey, "new", newKey)
}
