package effectiveconfig

import (
    "context"
    "database/sql"
    "errors"
    "log/slog"
    "time"
)

type Service struct {
    store    *Store
    client   *Client
    logger   *slog.Logger
    interval time.Duration
}

func NewService(store *Store, client *Client, logger *slog.Logger, interval time.Duration) *Service {
    if interval <= 0 { interval = time.Minute }
    return &Service{store: store, client: client, logger: logger, interval: interval}
}

func (s *Service) Refresh(ctx context.Context) (bool, error) {
    if s == nil || s.client == nil { return false, errors.New("central configuration refresh is disabled") }
    currentETag := ""
    if snapshot, err := s.store.Get(ctx); err == nil {
        currentETag = snapshot.ETag
    } else if !errors.Is(err, sql.ErrNoRows) {
        _ = s.store.RecordAttempt(ctx, err)
        return false, err
    }

    result, err := s.client.Fetch(ctx, currentETag)
    if err != nil {
        _ = s.store.RecordAttempt(ctx, err)
        return false, err
    }
    if !result.Changed {
        _ = s.store.RecordSuccess(ctx, currentETag)
        return false, nil
    }
    if err := s.store.Save(ctx, result.Snapshot); err != nil {
        _ = s.store.RecordAttempt(ctx, err)
        return false, err
    }
    if err := s.store.RecordSuccess(ctx, result.Snapshot.ETag); err != nil { return true, err }
    return true, nil
}

func (s *Service) Run(ctx context.Context) {
    if s == nil || s.client == nil { return }
    s.refreshAndLog(ctx)
    ticker := time.NewTicker(s.interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C: s.refreshAndLog(ctx)
        }
    }
}

func (s *Service) refreshAndLog(ctx context.Context) {
    changed, err := s.Refresh(ctx)
    if err != nil {
        if s.logger != nil { s.logger.Warn("effective configuration refresh failed", "error", err) }
        return
    }
    if changed && s.logger != nil { s.logger.Info("effective configuration snapshot updated") }
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) { return s.store.Get(ctx) }
func (s *Service) State(ctx context.Context) (SyncState, error) { return s.store.State(ctx) }
func (s *Service) Enabled() bool { return s != nil && s.client != nil }
