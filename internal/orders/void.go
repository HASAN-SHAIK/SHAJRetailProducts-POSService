package orders

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrAlreadyVoided = errors.New("order already voided")
	ErrRefundRequired = errors.New("completed order requires refund")
)

// VoidHook extends the atomic pre-completion void transaction. Hooks are used
// for invariants owned by adjacent modules (for example, Payments must prove
// that no captured balance remains) without moving those business rules into
// the Orders package.
type VoidHook func(context.Context, *sql.Tx, Order) error

// VoidWith cancels an order only before sale completion. Completed/returned
// sales are deliberately excluded: those require the refund/reversal workflow
// because inventory, payments and receipt facts may already be durable.
func (s *Service) VoidWith(ctx context.Context, id, approvedByUserID, reason string, hooks ...VoidHook) (Order, error) {
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

	approvedByUserID = strings.TrimSpace(approvedByUserID)
	reason = strings.TrimSpace(reason)
	if approvedByUserID == "" || reason == "" {
		return Order{}, ErrInvalidOrder
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	order.Status = "cancelled"
	order.UpdatedAt = now
	order.Version++

	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
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
			SET status='cancelled', version=?, updated_at=?, approved_by_user_id=?, approval_reason=?
			WHERE id=?`,
			order.Version, now, approvedByUserID, reason, id,
		); err != nil {
			return err
		}
		return s.saveSnapshot(ctx, tx, order)
	}); err != nil {
		return Order{}, err
	}
	return order, nil
}
