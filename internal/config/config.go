package config

import (
    "fmt"
    "os"
    "time"
)

type Config struct {
    Environment        string
    ListenAddress      string
    DatabasePath       string
    CentralAPIURL      string
    SyncRequestTimeout time.Duration
    SyncPollInterval   time.Duration
}

func Load() (Config, error) {
    requestTimeout, err := durationEnv("POS_SYNC_REQUEST_TIMEOUT", 10*time.Second)
    if err != nil { return Config{}, err }
    pollInterval, err := durationEnv("POS_SYNC_POLL_INTERVAL", 2*time.Second)
    if err != nil { return Config{}, err }

    cfg := Config{
        Environment:        envOrDefault("POS_ENVIRONMENT", "development"),
        ListenAddress:      envOrDefault("POS_SERVICE_ADDRESS", "127.0.0.1:4782"),
        DatabasePath:       envOrDefault("POS_DATABASE_PATH", "./data/shajretail-pos.db"),
        CentralAPIURL:      os.Getenv("POS_CENTRAL_API_URL"),
        SyncRequestTimeout: requestTimeout,
        SyncPollInterval:   pollInterval,
    }

    if cfg.ListenAddress == "" { return Config{}, fmt.Errorf("POS_SERVICE_ADDRESS cannot be empty") }
    if cfg.DatabasePath == "" { return Config{}, fmt.Errorf("POS_DATABASE_PATH cannot be empty") }
    if cfg.SyncRequestTimeout <= 0 { return Config{}, fmt.Errorf("POS_SYNC_REQUEST_TIMEOUT must be positive") }
    if cfg.SyncPollInterval <= 0 { return Config{}, fmt.Errorf("POS_SYNC_POLL_INTERVAL must be positive") }
    return cfg, nil
}

func envOrDefault(key, fallback string) string {
    if value := os.Getenv(key); value != "" { return value }
    return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
    raw := os.Getenv(key)
    if raw == "" { return fallback, nil }
    value, err := time.ParseDuration(raw)
    if err != nil { return 0, fmt.Errorf("%s: %w", key, err) }
    return value, nil
}
