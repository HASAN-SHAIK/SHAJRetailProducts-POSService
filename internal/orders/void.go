package orders

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrAlreadyVoided           = errors.New("order already voided")
	ErrRefundRequired          = errors.New("completed order requires refund")
	ErrPaymentReversalRequired = errors.New("captured payment requires reversal")
)

// VoidHook extends the atomic pre-completion void transaction for adjacent
// invariants that may be added later without weakening the order transition.
type VoidHook func(context.Context, *sql.Tx, Order) error

// VoidWith preserves the legacy single-actor contract by treating the
// approving user as the initiating user. New request paths should call
// VoidWithActors so operator and manager approver remain distinct.
func (s *Service) VoidWith(ctx context.Context, id, approvedByUserID, reason string, hooks ...VoidHook) (Order, error) {
	return s.VoidWithActors(ctx, id, approvedByUserID, approvedByUserID, reason, hooks...)
}

// VoidWithActors cancels an order only before sale completion and only when no
// net captured money remains, while durably preserving both the initiating
// operator and the approving manager in the same SQLite transaction.
func (s *Service) VoidWithActors(ctx context.Context, id, voidedByUserID, approvedByUserID, reason string, hooks ...VoidHook) (Order, error) {
	order, err := s.Get(ctx, id)
	if err != nil {
		return Order{}, err
	}
	if order.Status == "cancelled" {
		return Order{}, ErrAlreadyVoided
	}
	if order.CompletedAt != nil || order.Status == "returned" {
		return Order{}, ErrRefundRequired
	}

	voidedByUserID = strings.TrimSpace(voidedByUserID)
	approvedByUserID = strings.TrimSpace(approvedByUserID)
	reason = strings.TrimSpace(reason)
	if voidedByUserID == "" || approvedByUserID == "" || reason == "" {
		return Order{}, ErrInvalidOrder
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	order.Status = "cancelled"
	order.UpdatedAt = now
	order.Version++

	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var paidMinor int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(
				CASE
					WHEN status='captured' AND direction='in' THEN amount_minor
					WHEN status IN ('captured','refunded') AND direction='out' THEN -amount_minor
					ELSE 0
				END
			),0)
			FROM payments WHERE order_id=?`, order.ID).Scan(&paidMinor); err != nil {
			return err
		}
		if paidMinor != 0 {
			return ErrPaymentReversalRequired
		}

		for _, hook := range hooks {
			if hook == nil {
				continue
			}
			if err := hook(ctx, tx, order); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE sales_orders
			SET status='cancelled', version=?, updated_at=?, voided_by_user_id=?, approved_by_user_id=?, approval_reason=?
			WHERE id=?`,
			order.Version, now, voidedByUserID, approvedByUserID, reason, id,
		); err != nil {
			return err
		}
		return s.saveSnapshot(ctx, tx, order)
	}); err != nil {
		return Order{}, err
	}
	return order, nil
}
