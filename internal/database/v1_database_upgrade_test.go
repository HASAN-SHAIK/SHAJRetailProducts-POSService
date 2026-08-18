package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func applyHistoricalMigrationForV1Acceptance(ctx context.Context, db *DB, m migration) error {
	return db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version,name,checksum,applied_at) VALUES(?,?,?,?)`,
			m.Version, m.Name, m.Checksum, time.Now().UTC().Format(time.RFC3339Nano),
		)
		return err
	})
}

func TestV1RepresentativePreviousSchemaUpgradePreservesDurableFacts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pos-upgrade.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		db.Close()
		t.Fatalf("load embedded migrations: %v", err)
	}
	if len(migrations) < 10 {
		db.Close()
		t.Fatalf("need representative historical schema with local auth; found %d migrations", len(migrations))
	}

	for _, m := range migrations[:len(migrations)-1] {
		if err := applyHistoricalMigrationForV1Acceptance(ctx, db, m); err != nil {
			db.Close()
			t.Fatalf("apply historical migration %d (%s): %v", m.Version, m.Name, err)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO service_metadata(key,value_json,updated_at) VALUES(?,?,?)`,
		"upgrade-probe", `{"preserve":true}`, now); err != nil {
		db.Close()
		t.Fatalf("seed metadata fact: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO outbox_events(
		id, aggregate_type, aggregate_id, aggregate_version, event_type, schema_version,
		ordering_key, payload_json, metadata_json, status, attempt_count, available_at, created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evt-upgrade-1", "order", "order-upgrade-1", 1, "sale.completed", 1,
		"order:order-upgrade-1", `{"order_id":"order-upgrade-1"}`, `{}`, "pending", 0, now, now,
	); err != nil {
		db.Close()
		t.Fatalf("seed outbox fact: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inbox_messages(
		message_id, message_type, schema_version, source, payload_json, status, attempt_count, received_at, applied_at
	) VALUES(?,?,?,?,?,?,?,?,?)`,
		"msg-upgrade-1", "catalog.product.upsert", 1, "central", `{"product_id":"101"}`, "applied", 1, now, now,
	); err != nil {
		db.Close()
		t.Fatalf("seed inbox fact: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO local_users(
		user_id, tenant_id, role, branch_id, all_branch_access, permissions_json,
		pin_salt, pin_hash, pin_iterations, failed_attempts, grant_id, grant_expires_at, enabled, updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"user-upgrade-1", "tenant-upgrade", "cashier", "branch-a", 0, `[]`,
		[]byte("salt"), []byte("hash"), 120000, 0, "grant-upgrade-1", "2099-01-01T00:00:00Z", 1, now,
	); err != nil {
		db.Close()
		t.Fatalf("seed local auth fact: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close historical database: %v", err)
	}

	upgraded, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen historical database: %v", err)
	}
	defer upgraded.Close()
	if err := upgraded.Migrate(ctx); err != nil {
		t.Fatalf("upgrade previous schema to current: %v", err)
	}
	if err := upgraded.IntegrityCheck(ctx); err != nil {
		t.Fatalf("upgraded database integrity: %v", err)
	}

	var migrationCount int
	if err := upgraded.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count upgraded migration ledger: %v", err)
	}
	if migrationCount != len(migrations) {
		t.Fatalf("upgraded migration ledger count = %d, want %d", migrationCount, len(migrations))
	}

	assertCount := func(label, query string) {
		t.Helper()
		var count int
		if err := upgraded.SQL().QueryRowContext(ctx, query).Scan(&count); err != nil {
			t.Fatalf("read preserved %s fact: %v", label, err)
		}
		if count != 1 {
			t.Fatalf("preserved %s count = %d, want 1", label, count)
		}
	}
	assertCount("metadata", `SELECT COUNT(*) FROM service_metadata WHERE key='upgrade-probe'`)
	assertCount("outbox", `SELECT COUNT(*) FROM outbox_events WHERE id='evt-upgrade-1' AND status='pending'`)
	assertCount("inbox", `SELECT COUNT(*) FROM inbox_messages WHERE message_id='msg-upgrade-1' AND status='applied'`)
	assertCount("local auth", `SELECT COUNT(*) FROM local_users WHERE user_id='user-upgrade-1' AND grant_id='grant-upgrade-1'`)
}
