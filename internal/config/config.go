package config

import "os"

type Config struct {
    Address      string
    DatabasePath string
    Environment  string
}

func Load() Config {
    return Config{
        Address:      envOrDefault("POS_SERVICE_ADDRESS", "127.0.0.1:4782"),
        DatabasePath: envOrDefault("POS_DATABASE_PATH", "./data/shajretail-pos.db"),
        Environment:  envOrDefault("POS_ENVIRONMENT", "development"),
    }
}

func envOrDefault(key, fallback string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return fallback
}
