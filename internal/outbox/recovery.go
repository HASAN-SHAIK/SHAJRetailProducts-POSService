package outbox

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"
    "time"
)

var (
    ErrDeadLetterNotFound = errors.New("dead-letter outbox event not found")
    ErrRecoveryOrderingMismatch = errors.New("outbox recovery ordering key mismatch")
    ErrRecoveryAlreadyConsumed = errors.New("sync recovery authorization already consumed")
    ErrInvalidRecoveryAuthorization = errors.New("invalid sync recovery authorization")
)

type RecoveryAuthorization struct {
    RecoveryID string
    EventID string
    OrderingKey string
    OrderID string
    ApprovedByUserID string
    Reason string
}

// RequeueDeadLetter re-enables exactly one poisoned event after an external
// authority has approved recovery. It never skips or publishes the event, and
// does not alter later facts on the same ordering key.
func (s *Service) RequeueDeadLetter(ctx context.Context, eventID, orderingKey string) error {
    return s.db.WithTx(ctx, func(tx *sql.Tx) error {
        return requeueDeadLetterTx(ctx, tx, eventID, orderingKey)
    })
}

// ApplyAuthorizedRecovery consumes one Central-issued recovery authorization
// exactly once and requeues its exact dead-letter event in the same SQLite
// transaction. POS does not create authority here; it only persists that a
// verified Central authorization has already been used.
func (s *Service) ApplyAuthorizedRecovery(ctx context.Context, auth RecoveryAuthorization) error {
    auth.RecoveryID = strings.TrimSpace(auth.RecoveryID)
    auth.EventID = strings.TrimSpace(auth.EventID)
    auth.OrderingKey = strings.TrimSpace(auth.OrderingKey)
    auth.OrderID = strings.TrimSpace(auth.OrderID)
    auth.ApprovedByUserID = strings.TrimSpace(auth.ApprovedByUserID)
    auth.Reason = strings.TrimSpace(auth.Reason)
    if auth.RecoveryID == "" || auth.EventID == "" || auth.OrderID == "" || auth.OrderingKey != "sales_order:"+auth.OrderID || auth.ApprovedByUserID == "" || auth.Reason == "" {
        return ErrInvalidRecoveryAuthorization
    }

    return s.db.WithTx(ctx, func(tx *sql.Tx) error {
        var existingEventID, existingOrderingKey string
        err := tx.QueryRowContext(ctx, `SELECT event_id,ordering_key FROM pos_sync_recoveries WHERE recovery_id=?`, auth.RecoveryID).Scan(&existingEventID, &existingOrderingKey)
        if err == nil {
            return ErrRecoveryAlreadyConsumed
        }
        if !errors.Is(err, sql.ErrNoRows) {
            return fmt.Errorf("read sync recovery audit: %w", err)
        }

        if err := requeueDeadLetterTx(ctx, tx, auth.EventID, auth.OrderingKey); err != nil {
            return err
        }
        now := time.Now().UTC().Format(time.RFC3339Nano)
        if _, err := tx.ExecContext(ctx, `
            INSERT INTO pos_sync_recoveries(
                recovery_id,event_id,ordering_key,order_id,approved_by_user_id,reason,consumed_at
            ) VALUES(?,?,?,?,?,?,?)`,
            auth.RecoveryID, auth.EventID, auth.OrderingKey, auth.OrderID,
            auth.ApprovedByUserID, auth.Reason, now,
        ); err != nil {
            return fmt.Errorf("record sync recovery audit: %w", err)
        }
        return nil
    })
}

func requeueDeadLetterTx(ctx context.Context, tx *sql.Tx, eventID, orderingKey string) error {
    var currentOrderingKey string
    err := tx.QueryRowContext(ctx, `SELECT ordering_key FROM outbox_events WHERE id=? AND status='dead_letter'`, eventID).Scan(&currentOrderingKey)
    if errors.Is(err, sql.ErrNoRows) { return ErrDeadLetterNotFound }
    if err != nil { return fmt.Errorf("read dead-letter event: %w", err) }
    if currentOrderingKey != orderingKey { return ErrRecoveryOrderingMismatch }

    now := time.Now().UTC().Format(time.RFC3339Nano)
    result, err := tx.ExecContext(ctx, `
        UPDATE outbox_events
        SET status='pending', attempt_count=0, available_at=?, locked_at=NULL, lock_owner=NULL, last_error=NULL
        WHERE id=? AND status='dead_letter' AND ordering_key=?`, now, eventID, orderingKey)
    if err != nil { return fmt.Errorf("requeue dead-letter event: %w", err) }
    affected, err := result.RowsAffected()
    if err != nil { return err }
    if affected != 1 { return ErrDeadLetterNotFound }
    return nil
}
