package backup

import (
    "context"
    "log/slog"
    "time"
)

func (s *Service) Run(ctx context.Context, interval time.Duration) {
    if interval <= 0 { return }
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    create := func() {
        backupCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
        defer cancel()
        snapshot, err := s.Create(backupCtx)
        if err != nil {
            if ctx.Err() == nil { slog.Warn("POS backup failed", "error", err) }
            return
        }
        slog.Info("POS backup created", "path", snapshot.Path, "size_bytes", snapshot.SizeBytes)
    }

    create()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C: create()
        }
    }
}
