package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestV1BackupRetentionPrunesOldestAndPreservesNewestDurableState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	livePath := filepath.Join(root, "live", "pos.db")
	backupDir := filepath.Join(root, "backups")

	db, err := database.Open(ctx, livePath)
	if err != nil {
		t.Fatalf("open live sqlite database: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate live sqlite database: %v", err)
	}

	service := New(db, backupDir, 2)
	first, err := service.Create(ctx)
	if err != nil {
		t.Fatalf("create first snapshot: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	second, err := service.Create(ctx)
	if err != nil {
		t.Fatalf("create second snapshot: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO outbox_events(
		id, aggregate_type, aggregate_id, aggregate_version, event_type, schema_version,
		ordering_key, payload_json, metadata_json, status, attempt_count, available_at, created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evt-retention-latest", "order", "order-retention-latest", 1, "sale.completed", 1,
		"order:order-retention-latest", `{"order_id":"order-retention-latest"}`, `{}`, "pending", 0, now, now,
	); err != nil {
		t.Fatalf("seed durable state before newest snapshot: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	third, err := service.Create(ctx)
	if err != nil {
		t.Fatalf("create third snapshot: %v", err)
	}

	if _, err := os.Stat(first.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oldest snapshot still exists after retention prune: stat err=%v", err)
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup directory: %v", err)
	}
	var retained []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".db" {
			retained = append(retained, filepath.Join(backupDir, entry.Name()))
		}
	}
	sort.Strings(retained)
	want := []string{second.Path, third.Path}
	sort.Strings(want)
	if len(retained) != len(want) {
		t.Fatalf("retained snapshot count = %d, want %d: %v", len(retained), len(want), retained)
	}
	for i := range want {
		if retained[i] != want[i] {
			t.Fatalf("retained snapshots = %v, want %v", retained, want)
		}
	}

	if err := ValidateRestoreCandidate(ctx, third.Path); err != nil {
		t.Fatalf("validate newest retained snapshot: %v", err)
	}

	assertBackupOutboxCount(t, ctx, second.Path, "evt-retention-latest", 0)
	assertBackupOutboxCount(t, ctx, third.Path, "evt-retention-latest", 1)
}

func assertBackupOutboxCount(t *testing.T, ctx context.Context, path, eventID string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open retained snapshot %s: %v", filepath.Base(path), err)
	}
	defer db.Close()

	var got int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE id = ?`, eventID).Scan(&got); err != nil {
		t.Fatalf("read durable outbox state from %s: %v", filepath.Base(path), err)
	}
	if got != want {
		t.Fatalf("outbox event %q count in %s = %d, want %d", eventID, filepath.Base(path), got, want)
	}
}
