package database

import (
    "context"
    "crypto/sha256"
    "database/sql"
    "embed"
    "encoding/hex"
    "errors"
    "fmt"
    "io/fs"
    "path/filepath"
    "sort"
    "strconv"
    "strings"
    "time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
    Version  int
    Name     string
    SQL      string
    Checksum string
}

func (d *DB) Migrate(ctx context.Context) error {
    if _, err := d.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        checksum TEXT NOT NULL,
        applied_at TEXT NOT NULL
    )`); err != nil {
        return fmt.Errorf("bootstrap schema migrations table: %w", err)
    }

    migrations, err := loadMigrations()
    if err != nil {
        return err
    }

    for _, m := range migrations {
        var existing string
        err := d.sql.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version = ?`, m.Version).Scan(&existing)
        if err == nil {
            if existing != m.Checksum {
                return fmt.Errorf("migration %d checksum mismatch", m.Version)
            }
            continue
        }
        if !errors.Is(err, sql.ErrNoRows) {
            return fmt.Errorf("read migration %d: %w", m.Version, err)
        }

        if err := d.WithTx(ctx, func(tx *sql.Tx) error {
            if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
                return fmt.Errorf("apply migration %d: %w", m.Version, err)
            }
            if _, err := tx.ExecContext(ctx,
                `INSERT INTO schema_migrations(version,name,checksum,applied_at) VALUES(?,?,?,?)`,
                m.Version, m.Name, m.Checksum, time.Now().UTC().Format(time.RFC3339Nano),
            ); err != nil {
                return fmt.Errorf("record migration %d: %w", m.Version, err)
            }
            return nil
        }); err != nil {
            return err
        }
    }
    return nil
}

func loadMigrations() ([]migration, error) {
    entries, err := fs.ReadDir(migrationFiles, "migrations")
    if err != nil {
        return nil, fmt.Errorf("read migrations: %w", err)
    }
    out := make([]migration, 0, len(entries))
    for _, entry := range entries {
        if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
            continue
        }
        prefix, _, ok := strings.Cut(entry.Name(), "_")
        if !ok {
            return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
        }
        version, err := strconv.Atoi(prefix)
        if err != nil {
            return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
        }
        raw, err := migrationFiles.ReadFile("migrations/" + entry.Name())
        if err != nil {
            return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
        }
        sum := sha256.Sum256(raw)
        out = append(out, migration{Version: version, Name: entry.Name(), SQL: string(raw), Checksum: hex.EncodeToString(sum[:])})
    }
    sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
    return out, nil
}
