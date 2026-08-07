package config

import (
    "fmt"
    "net"
    "os"
    "strconv"
    "strings"
    "time"
)

type Config struct {
    Environment           string
    ListenAddress         string
    DatabasePath          string
    CentralAPIURL         string
    SyncRequestTimeout    time.Duration
    SyncPollInterval      time.Duration
    LocalAPIToken         string
    LocalTokenFile        string
    AllowedOrigins        []string
    BackupDirectory       string
    BackupInterval        time.Duration
    BackupRetention       int
    ObservabilityInterval time.Duration
}

func Load() (Config, error) {
    requestTimeout, err := durationEnv("POS_SYNC_REQUEST_TIMEOUT", 10*time.Second); if err != nil { return Config{}, err }
    pollInterval, err := durationEnv("POS_SYNC_POLL_INTERVAL", 2*time.Second); if err != nil { return Config{}, err }
    backupInterval, err := durationEnv("POS_BACKUP_INTERVAL", 6*time.Hour); if err != nil { return Config{}, err }
    observabilityInterval, err := durationEnv("POS_OBSERVABILITY_INTERVAL", 30*time.Second); if err != nil { return Config{}, err }
    retention, err := intEnv("POS_BACKUP_RETENTION", 14); if err != nil { return Config{}, err }

    databasePath := envOrDefault("POS_DATABASE_PATH", "./data/shajretail-pos.db")
    cfg := Config{
        Environment: envOrDefault("POS_ENVIRONMENT", "development"), ListenAddress: envOrDefault("POS_SERVICE_ADDRESS", "127.0.0.1:4782"),
        DatabasePath: databasePath, CentralAPIURL: os.Getenv("POS_CENTRAL_API_URL"), SyncRequestTimeout: requestTimeout, SyncPollInterval: pollInterval,
        LocalAPIToken: os.Getenv("POS_LOCAL_API_TOKEN"), LocalTokenFile: envOrDefault("POS_LOCAL_TOKEN_FILE", databasePath+".token"),
        AllowedOrigins: csvEnv("POS_ALLOWED_ORIGINS", []string{"http://localhost:3000","http://127.0.0.1:3000","http://localhost:5173","http://127.0.0.1:5173"}),
        BackupDirectory: envOrDefault("POS_BACKUP_DIRECTORY", databasePath+".backups"), BackupInterval: backupInterval, BackupRetention: retention,
        ObservabilityInterval: observabilityInterval,
    }
    if cfg.ListenAddress == "" { return Config{}, fmt.Errorf("POS_SERVICE_ADDRESS cannot be empty") }
    if cfg.DatabasePath == "" { return Config{}, fmt.Errorf("POS_DATABASE_PATH cannot be empty") }
    if cfg.LocalTokenFile == "" { return Config{}, fmt.Errorf("POS_LOCAL_TOKEN_FILE cannot be empty") }
    if cfg.BackupDirectory == "" { return Config{}, fmt.Errorf("POS_BACKUP_DIRECTORY cannot be empty") }
    if cfg.SyncRequestTimeout <= 0 || cfg.SyncPollInterval <= 0 || cfg.BackupInterval <= 0 || cfg.ObservabilityInterval <= 0 { return Config{}, fmt.Errorf("configured durations must be positive") }
    if cfg.BackupRetention <= 0 { return Config{}, fmt.Errorf("POS_BACKUP_RETENTION must be positive") }
    if err := validateLoopbackAddress(cfg.ListenAddress); err != nil { return Config{}, err }
    if len(cfg.AllowedOrigins) == 0 { return Config{}, fmt.Errorf("POS_ALLOWED_ORIGINS must contain at least one origin") }
    return cfg, nil
}

func validateLoopbackAddress(address string) error { host,_,err:=net.SplitHostPort(address); if err!=nil{return fmt.Errorf("POS_SERVICE_ADDRESS: %w",err)}; if strings.EqualFold(host,"localhost"){return nil}; ip:=net.ParseIP(host); if ip==nil||!ip.IsLoopback(){return fmt.Errorf("POS_SERVICE_ADDRESS must bind to loopback only")}; return nil }
func envOrDefault(key,fallback string) string { if value:=os.Getenv(key); value!="" { return value }; return fallback }
func csvEnv(key string,fallback []string) []string { raw:=strings.TrimSpace(os.Getenv(key)); if raw=="" { return append([]string(nil),fallback...) }; parts:=strings.Split(raw,","); out:=make([]string,0,len(parts)); for _,part:=range parts { if value:=strings.TrimRight(strings.TrimSpace(part),"/"); value!="" { out=append(out,value) } }; return out }
func durationEnv(key string,fallback time.Duration)(time.Duration,error){ raw:=os.Getenv(key); if raw==""{return fallback,nil}; value,err:=time.ParseDuration(raw); if err!=nil{return 0,fmt.Errorf("%s: %w",key,err)}; return value,nil }
func intEnv(key string,fallback int)(int,error){ raw:=strings.TrimSpace(os.Getenv(key)); if raw==""{return fallback,nil}; value,err:=strconv.Atoi(raw); if err!=nil{return 0,fmt.Errorf("%s: %w",key,err)}; return value,nil }
