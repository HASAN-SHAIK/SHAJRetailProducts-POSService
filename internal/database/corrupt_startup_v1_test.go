package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenRejectsCorruptedSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt-pos.db")
	if err := os.WriteFile(path, []byte("SHAJ-cycle-c-not-a-sqlite-database\ncorrupt-payload"), 0o600); err != nil {
		t.Fatalf("write corrupt sqlite fixture: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := Open(ctx, path)
	if db != nil {
		_ = db.Close()
	}
	if err == nil {
		t.Fatal("expected corrupted SQLite file to fail closed during Open")
	}
}
