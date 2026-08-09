package outbox

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"
)

var (
    ErrDeadLetterNotFound = errors.New("dead-letter outbox event not found")
    ErrRecoveryOrderingMismatch = errors.New("outbox recovery ordering key mismatch")
)

// RequeueDeadLetter re-enables exactly one poisoned event after an external
// authority has approved recovery. It never skips or publishes the event, and
// does not alter later facts on the same ordering key.
func (s *Service) RequeueDeadLetter(ctx context.Context, eventID, orderingKey string) error {
    var currentOrderingKey string
    err := s.db.SQL().QueryRowContext(ctx, `SELECT ordering_key FROM outbox_events WHERE id=? AND status='dead_letter'`, eventID).Scan(&currentOrderingKey)
    if errors.Is(err, sql.ErrNoRows) { return ErrDeadLetterNotFound }
    if err != nil { return fmt.Errorf("read dead-letter event: %w", err) }
    if currentOrderingKey != orderingKey { return ErrRecoveryOrderingMismatch }

    now := time.Now().UTC().Format(time.RFC3339Nano)
    result, err := s.db.SQL().ExecContext(ctx, `
        UPDATE outbox_events
        SET status='pending', attempt_count=0, available_at=?, locked_at=NULL, lock_owner=NULL, last_error=NULL
        WHERE id=? AND status='dead_letter' AND ordering_key=?`, now, eventID, orderingKey)
    if err != nil { return fmt.Errorf("requeue dead-letter event: %w", err) }
    affected, err := result.RowsAffected()
    if err != nil { return err }
    if affected != 1 { return ErrDeadLetterNotFound }
    return nil
}
