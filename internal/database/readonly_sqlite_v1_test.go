package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadOnlySQLiteRejectsDurableWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readonly-pos.db")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open writable sqlite fixture: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `CREATE TABLE write_probe(id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		t.Fatalf("create write probe: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO write_probe(id,value) VALUES(1,'before')`); err != nil {
		_ = db.Close()
		t.Fatalf("seed write probe: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close writable sqlite fixture: %v", err)
	}
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatalf("chmod database read-only: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod database directory read-only: %v", err)
	}
	defer os.Chmod(dir, 0o755)

	ro, err := Open(ctx, path)
	if err != nil {
		return
	}
	defer ro.Close()

	err = ro.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE write_probe SET value='after' WHERE id=1`)
		return err
	})
	if err == nil {
		t.Fatal("expected durable write against read-only SQLite to fail")
	}
}
