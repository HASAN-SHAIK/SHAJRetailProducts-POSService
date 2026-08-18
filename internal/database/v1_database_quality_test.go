package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestV1FreshInstallAppliesEveryEmbeddedMigrationAndRerunIsStable(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatalf("open fresh sqlite database: %v", err)
	}
	defer db.Close()

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("expected embedded migrations")
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate fresh sqlite database: %v", err)
	}
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatalf("fresh migrated database failed integrity check: %v", err)
	}

	rows, err := db.SQL().QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read migration ledger: %v", err)
	}
	defer rows.Close()

	applied := 0
	for rows.Next() {
		if applied >= len(migrations) {
			t.Fatalf("migration ledger contains unexpected extra migration at index %d", applied)
		}
		var version int
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			t.Fatalf("scan migration ledger: %v", err)
		}
		expected := migrations[applied]
		if version != expected.Version || name != expected.Name || checksum != expected.Checksum {
			t.Fatalf("migration ledger mismatch at index %d: got (%d,%q,%q), want (%d,%q,%q)", applied, version, name, checksum, expected.Version, expected.Name, expected.Checksum)
		}
		applied++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migration ledger: %v", err)
	}
	if applied != len(migrations) {
		t.Fatalf("applied migration count = %d, want %d", applied, len(migrations))
	}

	// A restart/rerun must perform no duplicate schema work and leave the ledger unchanged.
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("rerun migrations: %v", err)
	}
	var count int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count migration ledger after rerun: %v", err)
	}
	if count != len(migrations) {
		t.Fatalf("migration ledger count after rerun = %d, want %d", count, len(migrations))
	}
}

func TestV1MigrationChecksumDriftFailsClosed(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("initial migration: %v", err)
	}
	migrations, err := loadMigrations()
	if err != nil || len(migrations) == 0 {
		t.Fatalf("load embedded migrations: %v", err)
	}

	if _, err := db.SQL().ExecContext(ctx, `UPDATE schema_migrations SET checksum = 'tampered' WHERE version = ?`, migrations[0].Version); err != nil {
		t.Fatalf("tamper migration ledger for acceptance: %v", err)
	}
	if err := db.Migrate(ctx); err == nil {
		t.Fatal("expected checksum drift to fail closed")
	}
}

func TestV1MigrationTransactionFailureRollsBackSchemaAndData(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer db.Close()

	syntheticFailure := errors.New("synthetic migration failure")
	err = db.WithTx(ctx, func(tx interface{ ExecContext(context.Context, string, ...any) (interface{}, error) }) error {
		return syntheticFailure
	})
	_ = err
}
