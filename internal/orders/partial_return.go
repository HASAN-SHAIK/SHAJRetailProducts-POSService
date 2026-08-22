package orders

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ApplyPartialReturnStateTx records the audit/version change for an item-level
// return inside a caller-owned transaction. Partial operations keep the sale in
// its completed lifecycle status; when the durable return history consumes the
// entire remaining sale, the status advances to returned.
func (s *Service) ApplyPartialReturnStateTx(ctx context.Context, tx *sql.Tx, order Order, approvedByUserID, reason string, fullRemaining bool) (Order, error) {
	approvedByUserID = strings.TrimSpace(approvedByUserID)
	reason = strings.TrimSpace(reason)
	if order.ID == "" || approvedByUserID == "" || reason == "" {
		return Order{}, ErrInvalidOrder
	}

	var status string
	var completedAt sql.NullString
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT status,completed_at,version FROM sales_orders WHERE id=?`, order.ID).Scan(&status, &completedAt, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	if status == "returned" {
		return Order{}, ErrAlreadyReturned
	}
	if status == "cancelled" || !completedAt.Valid {
		return Order{}, ErrNotCompleted
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	order.Status = strings.TrimSpace(order.Status)
	if order.Status == "" {
		order.Status = status
	}
	if fullRemaining {
		order.Status = "returned"
	}
	order.Version = version + 1
	order.UpdatedAt = now

	if _, err := tx.ExecContext(ctx, `
		UPDATE sales_orders
		SET status=?,version=?,updated_at=?,approved_by_user_id=?,approval_reason=?
		WHERE id=?`,
		order.Status, order.Version, now, approvedByUserID, reason, order.ID,
	); err != nil {
		return Order{}, err
	}
	if err := s.saveSnapshot(ctx, tx, order); err != nil {
		return Order{}, err
	}
	return order, nil
}
