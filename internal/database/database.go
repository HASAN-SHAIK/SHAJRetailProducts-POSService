package database

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "time"

    _ "github.com/mattn/go-sqlite3"
)

const defaultBusyTimeout = 5 * time.Second

type DB struct {
    sql *sql.DB
}

func Open(ctx context.Context, path string) (*DB, error) {
    if path == "" {
        return nil, errors.New("database path is required")
    }
    if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
        return nil, fmt.Errorf("create database directory: %w", err)
    }

    dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL&_synchronous=FULL&_busy_timeout=%d&_txlock=immediate", path, defaultBusyTimeout.Milliseconds())
    handle, err := sql.Open("sqlite3", dsn)
    if err != nil {
        return nil, fmt.Errorf("open sqlite: %w", err)
    }
    handle.SetMaxOpenConns(1)
    handle.SetMaxIdleConns(1)
    handle.SetConnMaxLifetime(0)

    db := &DB{sql: handle}
    if err := db.Ping(ctx); err != nil {
        _ = handle.Close()
        return nil, err
    }
    if err := db.assertPragmas(ctx); err != nil {
        _ = handle.Close()
        return nil, err
    }
    return db, nil
}

func (d *DB) SQL() *sql.DB { return d.sql }

func (d *DB) Ping(ctx context.Context) error {
    if err := d.sql.PingContext(ctx); err != nil {
        return fmt.Errorf("ping sqlite: %w", err)
    }
    return nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
    tx, err := d.sql.BeginTx(ctx, &sql.TxOptions{})
    if err != nil {
        return fmt.Errorf("begin transaction: %w", err)
    }
    committed := false
    defer func() {
        if !committed {
            _ = tx.Rollback()
        }
    }()
    if err := fn(tx); err != nil {
        return err
    }
    if err := tx.Commit(); err != nil {
        return fmt.Errorf("commit transaction: %w", err)
    }
    committed = true
    return nil
}

func (d *DB) IntegrityCheck(ctx context.Context) error {
    var result string
    if err := d.sql.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
        return fmt.Errorf("run integrity check: %w", err)
    }
    if result != "ok" {
        return fmt.Errorf("sqlite integrity check failed: %s", result)
    }
    return nil
}

func (d *DB) assertPragmas(ctx context.Context) error {
    checks := map[string]string{
        "journal_mode": "wal",
        "foreign_keys": "1",
        "synchronous":  "2",
    }
    for pragma, expected := range checks {
        var actual string
        if err := d.sql.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&actual); err != nil {
            return fmt.Errorf("read pragma %s: %w", pragma, err)
        }
        if actual != expected {
            return fmt.Errorf("pragma %s=%s, expected %s", pragma, actual, expected)
        }
    }
    return nil
}
