package backup

import (
    "context"
    "database/sql"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    _ "github.com/mattn/go-sqlite3"
)

type Service struct {
    db        *database.DB
    directory string
    retain    int
}

type Snapshot struct {
    Path      string    `json:"path"`
    SizeBytes int64     `json:"size_bytes"`
    CreatedAt time.Time `json:"created_at"`
}

func New(db *database.DB, directory string, retain int) *Service {
    if retain <= 0 {
        retain = 14
    }
    return &Service{db: db, directory: directory, retain: retain}
}

func (s *Service) Create(ctx context.Context) (Snapshot, error) {
    if strings.TrimSpace(s.directory) == "" {
        return Snapshot{}, fmt.Errorf("backup directory is required")
    }
    if err := os.MkdirAll(s.directory, 0o750); err != nil {
        return Snapshot{}, fmt.Errorf("create backup directory: %w", err)
    }

    name := "shajretail-pos-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".db"
    finalPath := filepath.Join(s.directory, name)
    tempPath := finalPath + ".tmp"
    _ = os.Remove(tempPath)

    if _, err := s.db.SQL().ExecContext(ctx, "VACUUM INTO '"+escapeSQLiteLiteral(tempPath)+"'"); err != nil {
        return Snapshot{}, fmt.Errorf("create sqlite snapshot: %w", err)
    }
    if err := verifySQLite(ctx, tempPath); err != nil {
        _ = os.Remove(tempPath)
        return Snapshot{}, err
    }
    if err := os.Chmod(tempPath, 0o600); err != nil {
        _ = os.Remove(tempPath)
        return Snapshot{}, fmt.Errorf("secure backup permissions: %w", err)
    }
    if err := os.Rename(tempPath, finalPath); err != nil {
        _ = os.Remove(tempPath)
        return Snapshot{}, fmt.Errorf("publish backup snapshot: %w", err)
    }
    info, err := os.Stat(finalPath)
    if err != nil {
        return Snapshot{}, fmt.Errorf("stat backup: %w", err)
    }
    if err := s.prune(); err != nil {
        return Snapshot{}, err
    }
    return Snapshot{Path: finalPath, SizeBytes: info.Size(), CreatedAt: info.ModTime().UTC()}, nil
}

// ValidateRestoreCandidate verifies a backup before an operator replaces the
// live database. Restore is intentionally not performed while the service is
// running; the POS process must be stopped before replacing the database file.
func ValidateRestoreCandidate(ctx context.Context, path string) error {
    if strings.TrimSpace(path) == "" {
        return fmt.Errorf("restore path is required")
    }
    return verifySQLite(ctx, path)
}

func (s *Service) prune() error {
    entries, err := os.ReadDir(s.directory)
    if err != nil {
        return fmt.Errorf("read backup directory: %w", err)
    }
    var files []string
    for _, entry := range entries {
        if entry.IsDir() || !strings.HasPrefix(entry.Name(), "shajretail-pos-") || !strings.HasSuffix(entry.Name(), ".db") {
            continue
        }
        files = append(files, filepath.Join(s.directory, entry.Name()))
    }
    sort.Strings(files)
    if len(files) <= s.retain {
        return nil
    }
    for _, path := range files[:len(files)-s.retain] {
        if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
            return fmt.Errorf("prune backup %s: %w", path, err)
        }
    }
    return nil
}

func verifySQLite(ctx context.Context, path string) error {
    db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&_foreign_keys=on")
    if err != nil {
        return fmt.Errorf("open backup for verification: %w", err)
    }
    defer db.Close()
    var result string
    if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
        return fmt.Errorf("verify backup integrity: %w", err)
    }
    if result != "ok" {
        return fmt.Errorf("backup integrity check failed: %s", result)
    }
    var count int
    if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('sales_orders','outbox_events','schema_migrations')").Scan(&count); err != nil {
        return fmt.Errorf("verify backup schema: %w", err)
    }
    if count != 3 {
        return fmt.Errorf("backup is missing required POS tables")
    }
    return nil
}

func escapeSQLiteLiteral(v string) string { return strings.ReplaceAll(v, "'", "''") }
