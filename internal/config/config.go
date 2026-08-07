package config

import (
    "fmt"
    "os"
)

type Config struct {
    Environment   string
    ListenAddress string
    DatabasePath  string
    CentralAPIURL string
}

func Load() (Config, error) {
    cfg := Config{
        Environment:   envOrDefault("POS_ENVIRONMENT", "development"),
        ListenAddress: envOrDefault("POS_SERVICE_ADDRESS", "127.0.0.1:4782"),
        DatabasePath:  envOrDefault("POS_DATABASE_PATH", "./data/shajretail-pos.db"),
        CentralAPIURL: os.Getenv("POS_CENTRAL_API_URL"),
    }

    if cfg.ListenAddress == "" {
        return Config{}, fmt.Errorf("POS_SERVICE_ADDRESS cannot be empty")
    }
    if cfg.DatabasePath == "" {
        return Config{}, fmt.Errorf("POS_DATABASE_PATH cannot be empty")
    }

    return cfg, nil
}

func envOrDefault(key, fallback string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return fallback
}
