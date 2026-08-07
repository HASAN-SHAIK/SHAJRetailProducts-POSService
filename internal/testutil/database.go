package testutil

import (
    "context"
    "path/filepath"
    "testing"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func OpenDatabase(t *testing.T) *database.DB {
    t.Helper()
    ctx := context.Background()
    db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos-test.db"))
    if err != nil { t.Fatalf("open test database: %v", err) }
    if err := db.Migrate(ctx); err != nil { db.Close(); t.Fatalf("migrate test database: %v", err) }
    t.Cleanup(func() { _ = db.Close() })
    return db
}
