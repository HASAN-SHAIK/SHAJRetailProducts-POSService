package outbox

import (
    "context"
    "database/sql"
    "fmt"
    "math"
    "time"
)

const maxAttempts = 12

// ClaimNext atomically reserves one due event for a dispatcher instance.
func (s *Service) ClaimNext(ctx context.Context, owner string) (*Event, error) {
    var claimed *Event
    err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
        now := time.Now().UTC().Format(time.RFC3339Nano)
        var id string
        err := tx.QueryRowContext(ctx, `
            SELECT id FROM outbox_events
            WHERE status IN ('pending','failed') AND available_at <= ?
            ORDER BY created_at,id LIMIT 1`, now).Scan(&id)
        if err == sql.ErrNoRows { return nil }
        if err != nil { return err }

        result, err := tx.ExecContext(ctx, `
            UPDATE outbox_events
            SET status='processing', locked_at=?, lock_owner=?
            WHERE id=? AND status IN ('pending','failed')`, now, owner, id)
        if err != nil { return err }
        affected, err := result.RowsAffected()
        if err != nil { return err }
        if affected == 0 { return nil }

        e, err := loadEventTx(ctx, tx, id)
        if err != nil { return err }
        claimed = &e
        return nil
    })
    if err != nil { return nil, fmt.Errorf("claim outbox event: %w", err) }
    return claimed, nil
}

func (s *Service) MarkPublished(ctx context.Context, id, owner string) error {
    now := time.Now().UTC().Format(time.RFC3339Nano)
    result, err := s.db.SQL().ExecContext(ctx, `
        UPDATE outbox_events
        SET status='published', published_at=?, locked_at=NULL, lock_owner=NULL, last_error=NULL
        WHERE id=? AND status='processing' AND lock_owner=?`, now, id, owner)
    if err != nil { return fmt.Errorf("mark outbox published: %w", err) }
    affected, err := result.RowsAffected()
    if err != nil { return err }
    if affected != 1 { return fmt.Errorf("outbox publish ownership lost") }
    return nil
}

func (s *Service) MarkFailed(ctx context.Context, id, owner, reason string) error {
    if len(reason) > 2000 { reason = reason[:2000] }
    var attempts int
    err := s.db.SQL().QueryRowContext(ctx,
        `SELECT attempt_count FROM outbox_events WHERE id=? AND status='processing' AND lock_owner=?`, id, owner).Scan(&attempts)
    if err != nil { return fmt.Errorf("read outbox attempts: %w", err) }
    attempts++
    status := "failed"
    if attempts >= maxAttempts { status = "dead_letter" }
    next := time.Now().UTC().Add(backoff(attempts)).Format(time.RFC3339Nano)
    _, err = s.db.SQL().ExecContext(ctx, `
        UPDATE outbox_events
        SET status=?, attempt_count=?, available_at=?, locked_at=NULL, lock_owner=NULL, last_error=?
        WHERE id=? AND status='processing' AND lock_owner=?`, status, attempts, next, reason, id, owner)
    if err != nil { return fmt.Errorf("mark outbox failed: %w", err) }
    return nil
}

func (s *Service) RecoverStaleClaims(ctx context.Context, olderThan time.Duration) error {
    cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339Nano)
    _, err := s.db.SQL().ExecContext(ctx, `
        UPDATE outbox_events
        SET status='failed', locked_at=NULL, lock_owner=NULL,
            last_error=COALESCE(last_error,'dispatcher lease expired')
        WHERE status='processing' AND locked_at < ?`, cutoff)
    return err
}

func backoff(attempt int) time.Duration {
    if attempt < 1 { attempt = 1 }
    seconds := math.Pow(2, float64(attempt-1))
    if seconds > 300 { seconds = 300 }
    // Deterministic jitter keeps tests reproducible while avoiding synchronized retries.
    jitterMillis := (attempt*7919)%1000
    return time.Duration(seconds)*time.Second + time.Duration(jitterMillis)*time.Millisecond
}

func loadEventTx(ctx context.Context, tx *sql.Tx, id string) (Event, error) {
    var e Event
    var payload, metadata string
    err := tx.QueryRowContext(ctx, `
        SELECT id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
               payload_json,metadata_json,status,attempt_count,available_at,created_at,published_at
        FROM outbox_events WHERE id=?`, id).Scan(
        &e.ID,&e.AggregateType,&e.AggregateID,&e.AggregateVersion,&e.EventType,&e.SchemaVersion,&e.OrderingKey,
        &payload,&metadata,&e.Status,&e.AttemptCount,&e.AvailableAt,&e.CreatedAt,&e.PublishedAt)
    if err != nil { return Event{}, err }
    e.Payload=[]byte(payload); e.Metadata=[]byte(metadata)
    return e,nil
}
