package observability

import (
    "context"
    "log/slog"
    "time"
)

func (c *Collector) Run(ctx context.Context, interval time.Duration) {
    if interval <= 0 { interval = 30 * time.Second }
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    emit := func() {
        runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()
        snap, err := c.Collect(runCtx)
        if err != nil {
            if ctx.Err() == nil { slog.Warn("operational diagnostics collection failed", "error", err) }
            return
        }
        slog.Info("pos operational diagnostics",
            "database_ok", snap.DatabaseOK,
            "outbox_pending", snap.Outbox.Pending,
            "outbox_processing", snap.Outbox.Processing,
            "outbox_failed", snap.Outbox.Failed,
            "outbox_dead_letter", snap.Outbox.DeadLetter,
            "inbox_received", snap.InboxReceived,
            "inbox_failed", snap.InboxFailed,
            "customer_conflicts", snap.CustomerConflicts,
            "unsynced_customers", snap.UnsyncedCustomers,
            "latest_backup_at", snap.LatestBackupAt,
            "latest_backup_bytes", snap.LatestBackupBytes,
        )
    }

    emit()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C: emit()
        }
    }
}
