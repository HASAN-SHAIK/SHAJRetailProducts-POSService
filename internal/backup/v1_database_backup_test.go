package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestV1VerifiedSnapshotRestoresDurableSyncFacts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	livePath := filepath.Join(root, "live", "pos.db")
	backupDir := filepath.Join(root, "backups")

	db, err := database.Open(ctx, livePath)
	if err != nil {
		t.Fatalf("open live sqlite database: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate live sqlite database: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO outbox_events(
		id, aggregate_type, aggregate_id, aggregate_version, event_type, schema_version,
		ordering_key, payload_json, metadata_json, status, attempt_count, available_at, created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evt-backup-1", "order", "order-backup-1", 1, "sale.completed", 1,
		"order:order-backup-1", `{"order_id":"order-backup-1"}`, `{}`, "pending", 0, now, now,
	); err != nil {
		db.Close()
		t.Fatalf("seed durable outbox fact: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inbox_messages(
		message_id, message_type, schema_version, source, payload_json, status, attempt_count, received_at, applied_at
	) VALUES(?,?,?,?,?,?,?,?,?)`,
		"msg-backup-1", "catalog.product.upsert", 1, "central", `{"product_id":"101"}`, "applied", 1, now, now,
	); err != nil {
		db.Close()
		t.Fatalf("seed durable inbox fact: %v", err)
	}

	service := New(db, backupDir, 3)
	snapshot, err := service.Create(ctx)
	if err != nil {
		db.Close()
		t.Fatalf("create verified sqlite snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close live sqlite database: %v", err)
	}

	if snapshot.SizeBytes <= 0 {
		t.Fatalf("snapshot size = %d, want > 0", snapshot.SizeBytes)
	}
	if err := ValidateRestoreCandidate(ctx, snapshot.Path); err != nil {
		t.Fatalf("validate known-good restore candidate: %v", err)
	}
	info, err := os.Stat(snapshot.Path)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot permissions = %o, want 600", info.Mode().Perm())
	}

	// Simulate the documented offline restore: the service is stopped, the
	// validated snapshot replaces the configured database, then startup
	// migrations/integrity checks run before the API becomes usable.
	restoredPath := filepath.Join(root, "restored", "pos.db")
	if err := os.MkdirAll(filepath.Dir(restoredPath), 0o750); err != nil {
		t.Fatalf("create restore directory: %v", err)
	}
	raw, err := os.ReadFile(snapshot.Path)
	if err != nil {
		t.Fatalf("read snapshot for restore: %v", err)
	}
	if err := os.WriteFile(restoredPath, raw, 0o600); err != nil {
		t.Fatalf("replace restored database: %v", err)
	}

	restored, err := database.Open(ctx, restoredPath)
	if err != nil {
		t.Fatalf("open restored sqlite database: %v", err)
	}
	defer restored.Close()
	if err := restored.Migrate(ctx); err != nil {
		t.Fatalf("run startup migrations after restore: %v", err)
	}
	if err := restored.IntegrityCheck(ctx); err != nil {
		t.Fatalf("restored database integrity: %v", err)
	}

	var outboxCount int
	if err := restored.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE id = 'evt-backup-1' AND status = 'pending'`).Scan(&outboxCount); err != nil {
		t.Fatalf("read restored outbox fact: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("restored pending outbox count = %d, want 1", outboxCount)
	}

	var inboxCount int
	if err := restored.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inbox_messages WHERE message_id = 'msg-backup-1' AND status = 'applied'`).Scan(&inboxCount); err != nil {
		t.Fatalf("read restored inbox fact: %v", err)
	}
	if inboxCount != 1 {
		t.Fatalf("restored applied inbox count = %d, want 1", inboxCount)
	}
}

func TestV1RestoreValidationRejectsCorruptCandidate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt restore candidate: %v", err)
	}
	if err := ValidateRestoreCandidate(ctx, path); err == nil {
		t.Fatal("expected corrupt restore candidate to be rejected")
	}
}
