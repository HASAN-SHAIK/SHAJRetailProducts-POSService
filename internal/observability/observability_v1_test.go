package observability

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

func TestV1POSSummaryAndBackupDiagnostics(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite database: %v", err)
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO outbox_events(
			id, aggregate_type, aggregate_id, aggregate_version, event_type,
			schema_version, ordering_key, payload_json, metadata_json, status,
			attempt_count, available_at, created_at
		) VALUES
			('evt-pending','customer','cus-1',1,'customer.changed',1,'customer:cus-1','{}','{}','pending',0,?,?),
			('evt-dead','inventory','mov-1',1,'inventory.movement.recorded',1,'inventory:mov-1','{}','{}','dead_letter',12,?,?)`,
		nowText, nowText, nowText, nowText,
	); err != nil {
		t.Fatalf("seed outbox diagnostics: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inbox_messages(message_id,message_type,schema_version,source,payload_json,status,attempt_count,received_at,last_error)
		VALUES
			('msg-received','catalog.product.upsert',1,'central','{}','received',0,?,NULL),
			('msg-failed','catalog.product.upsert',1,'central','{}','failed',3,?,'projection failed')`,
		nowText, nowText,
	); err != nil {
		t.Fatalf("seed inbox diagnostics: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO customers(id,name,currency,status,created_at,updated_at,local_version,sync_state)
		VALUES
			('cus-pending','Pending Customer','INR','active',?,?,1,'pending'),
			('cus-conflict','Conflict Customer','INR','active',?,?,2,'conflict')`,
		nowText, nowText, nowText, nowText,
	); err != nil {
		t.Fatalf("seed customer diagnostics: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO sync_checkpoints(stream_name,cursor_value,updated_at)
		VALUES('central_changes','cursor-42',?)`, nowText,
	); err != nil {
		t.Fatalf("seed change cursor: %v", err)
	}

	backupDir := t.TempDir()
	older := filepath.Join(backupDir, "older.db")
	latest := filepath.Join(backupDir, "latest.db")
	if err := os.WriteFile(older, []byte("old"), 0o600); err != nil {
		t.Fatalf("write older backup: %v", err)
	}
	if err := os.WriteFile(latest, []byte("latest-backup"), 0o600); err != nil {
		t.Fatalf("write latest backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "ignore.txt"), []byte("not-a-backup"), 0o600); err != nil {
		t.Fatalf("write non-db file: %v", err)
	}
	if err := os.Chtimes(older, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("set older backup time: %v", err)
	}
	latestTime := now.Add(-5 * time.Minute).Truncate(time.Second)
	if err := os.Chtimes(latest, latestTime, latestTime); err != nil {
		t.Fatalf("set latest backup time: %v", err)
	}

	snapshot, err := New(db, outbox.New(db), backupDir).Collect(ctx)
	if err != nil {
		t.Fatalf("collect observability snapshot: %v", err)
	}
	if !snapshot.DatabaseOK {
		t.Fatal("expected database_ok=true")
	}
	if snapshot.Outbox.Pending != 1 || snapshot.Outbox.DeadLetter != 1 {
		t.Fatalf("unexpected outbox status: %+v", snapshot.Outbox)
	}
	if snapshot.InboxReceived != 1 || snapshot.InboxFailed != 1 {
		t.Fatalf("unexpected inbox counts: received=%d failed=%d", snapshot.InboxReceived, snapshot.InboxFailed)
	}
	if snapshot.CustomerConflicts != 1 || snapshot.UnsyncedCustomers != 1 {
		t.Fatalf("unexpected customer sync counts: conflicts=%d pending=%d", snapshot.CustomerConflicts, snapshot.UnsyncedCustomers)
	}
	if snapshot.LastChangeCursor == nil || *snapshot.LastChangeCursor != "cursor-42" {
		t.Fatalf("unexpected change cursor: %#v", snapshot.LastChangeCursor)
	}
	if snapshot.LatestBackupBytes != int64(len("latest-backup")) {
		t.Fatalf("latest backup bytes=%d, want %d", snapshot.LatestBackupBytes, len("latest-backup"))
	}
	if snapshot.LatestBackupAt == nil {
		t.Fatal("expected latest backup timestamp")
	}
	parsed, err := time.Parse(time.RFC3339Nano, *snapshot.LatestBackupAt)
	if err != nil {
		t.Fatalf("parse latest backup timestamp: %v", err)
	}
	if !parsed.Equal(latestTime) {
		t.Fatalf("latest backup timestamp=%s, want %s", parsed, latestTime)
	}
}
