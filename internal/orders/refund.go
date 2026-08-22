package orders

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrAlreadyReturned = errors.New("order already returned")
	ErrNotCompleted    = errors.New("order is not completed")
)

// RefundHook runs inside the same transaction as the final returned-order
// transition. paidMinor is the net captured amount that must be compensated.
type RefundHook func(context.Context, *sql.Tx, Order, int64) error

// RefundWith preserves the legacy single-actor contract by treating the
// approving user as the initiating user. New request paths should call
// RefundWithActors so cashier/operator and manager approver remain distinct.
func (s *Service) RefundWith(ctx context.Context, id, approvedByUserID, reason string, hooks ...RefundHook) (Order, error) {
	return s.RefundWithActors(ctx, id, approvedByUserID, approvedByUserID, reason, hooks...)
}

// RefundWithActors atomically turns a completed sale into a returned sale while
// preserving the initiating cashier/operator independently from the approving
// manager. Payment, inventory, audit persistence, snapshots, and outbox hooks
// participate in the same SQLite transaction.
func (s *Service) RefundWithActors(ctx context.Context, id, refundedByUserID, approvedByUserID, reason string, hooks ...RefundHook) (Order, error) {
	order, err := s.Get(ctx, id)
	if err != nil { return Order{}, err }
	if order.Status == "returned" { return Order{}, ErrAlreadyReturned }
	if order.Status == "cancelled" || order.CompletedAt == nil { return Order{}, ErrNotCompleted }

	refundedByUserID = strings.TrimSpace(refundedByUserID)
	approvedByUserID = strings.TrimSpace(approvedByUserID)
	reason = strings.TrimSpace(reason)
	if refundedByUserID == "" || approvedByUserID == "" || reason == "" { return Order{}, ErrInvalidOrder }

	now := time.Now().UTC().Format(time.RFC3339Nano)
	order.Status = "returned"
	order.UpdatedAt = now
	order.Version++

	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var status string
		var completedAt sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT status,completed_at FROM sales_orders WHERE id=?`, id).Scan(&status, &completedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) { return ErrNotFound }
			return err
		}
		if status == "returned" { return ErrAlreadyReturned }
		if status == "cancelled" || !completedAt.Valid { return ErrNotCompleted }

		var paidMinor int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(CASE
				WHEN status='captured' AND direction='in' THEN amount_minor
				WHEN status IN ('captured','refunded') AND direction='out' THEN -amount_minor
				ELSE 0 END),0)
			FROM payments WHERE order_id=?`, id).Scan(&paidMinor); err != nil { return err }
		if paidMinor < 0 { return ErrInvalidOrder }

		for _, hook := range hooks {
			if hook == nil { continue }
			if err := hook(ctx, tx, order, paidMinor); err != nil { return err }
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE sales_orders
			SET status='returned',version=?,updated_at=?,refunded_by_user_id=?,approved_by_user_id=?,approval_reason=?
			WHERE id=?`,
			order.Version, now, refundedByUserID, approvedByUserID, reason, id); err != nil { return err }
		return s.saveSnapshot(ctx, tx, order)
	}); err != nil { return Order{}, err }
	return order, nil
}
