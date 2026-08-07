package observability

import (
    "context"
    "database/sql"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

type Snapshot struct {
    CollectedAt        string        `json:"collected_at"`
    DatabaseOK         bool          `json:"database_ok"`
    Outbox             outbox.Status `json:"outbox"`
    InboxReceived      int64         `json:"inbox_received"`
    InboxFailed        int64         `json:"inbox_failed"`
    CustomerConflicts  int64         `json:"customer_conflicts"`
    UnsyncedCustomers  int64         `json:"unsynced_customers"`
    LastChangeCursor   *string       `json:"last_change_cursor,omitempty"`
    LatestBackupAt     *string       `json:"latest_backup_at,omitempty"`
    LatestBackupBytes  int64         `json:"latest_backup_bytes,omitempty"`
}

type Collector struct {
    db        *database.DB
    outbox    *outbox.Service
    backupDir string
}

func New(db *database.DB, eventOutbox *outbox.Service, backupDir string) *Collector {
    return &Collector{db: db, outbox: eventOutbox, backupDir: backupDir}
}

func (c *Collector) Collect(ctx context.Context) (Snapshot, error) {
    snap := Snapshot{CollectedAt: time.Now().UTC().Format(time.RFC3339Nano)}
    if err := c.db.Ping(ctx); err != nil {
        return snap, fmt.Errorf("database ping: %w", err)
    }
    snap.DatabaseOK = true

    st, err := c.outbox.GetStatus(ctx)
    if err != nil { return snap, fmt.Errorf("outbox status: %w", err) }
    snap.Outbox = st

    if err := c.db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inbox_messages WHERE status='received'`).Scan(&snap.InboxReceived); err != nil { return snap, err }
    if err := c.db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inbox_messages WHERE status='failed'`).Scan(&snap.InboxFailed); err != nil { return snap, err }
    if err := c.db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM customers WHERE sync_state='conflict'`).Scan(&snap.CustomerConflicts); err != nil { return snap, err }
    if err := c.db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM customers WHERE sync_state='pending'`).Scan(&snap.UnsyncedCustomers); err != nil { return snap, err }

    var cursor sql.NullString
    if err := c.db.SQL().QueryRowContext(ctx, `SELECT cursor_value FROM sync_checkpoints WHERE stream_name='central_changes'`).Scan(&cursor); err != nil && err != sql.ErrNoRows { return snap, err }
    if cursor.Valid { snap.LastChangeCursor = &cursor.String }

    latestAt, latestBytes, err := latestBackup(c.backupDir)
    if err != nil { return snap, err }
    snap.LatestBackupAt, snap.LatestBackupBytes = latestAt, latestBytes
    return snap, nil
}

func latestBackup(dir string) (*string, int64, error) {
    entries, err := os.ReadDir(dir)
    if os.IsNotExist(err) { return nil, 0, nil }
    if err != nil { return nil, 0, err }
    type candidate struct { mod time.Time; size int64 }
    var items []candidate
    for _, entry := range entries {
        if entry.IsDir() || filepath.Ext(entry.Name()) != ".db" { continue }
        info, err := entry.Info(); if err != nil { return nil, 0, err }
        items = append(items, candidate{mod: info.ModTime(), size: info.Size()})
    }
    if len(items)==0 { return nil,0,nil }
    sort.Slice(items, func(i,j int) bool { return items[i].mod.After(items[j].mod) })
    value := items[0].mod.UTC().Format(time.RFC3339Nano)
    return &value, items[0].size, nil
}
