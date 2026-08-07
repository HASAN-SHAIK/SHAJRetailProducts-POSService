package orders

import (
    "context"
    "database/sql"
    "time"
)

// CompletionHook extends the atomic sale-completion transaction. Inventory,
// receipt creation, and later outbox publication can all participate in the
// same SQLite commit without creating cross-module partial state.
type CompletionHook func(context.Context, *sql.Tx, Order) error

func (s *Service) CompleteWith(ctx context.Context, id string, hooks ...CompletionHook) (Order, error) {
    order, err := s.Get(ctx, id)
    if err != nil {
        return Order{}, err
    }
    if order.CompletedAt != nil || order.Status == "cancelled" || order.Status == "returned" {
        return Order{}, ErrAlreadyComplete
    }

    now := time.Now().UTC().Format(time.RFC3339Nano)
    order.CompletedAt = &now
    order.UpdatedAt = now
    order.Version++
    if order.Status == "draft" {
        order.Status = "confirmed"
    }

    if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
        for _, hook := range hooks {
            if hook == nil {
                continue
            }
            if err := hook(ctx, tx, order); err != nil {
                return err
            }
        }
        if _, err := tx.ExecContext(ctx,
            `UPDATE sales_orders SET completed_at=?, updated_at=?, version=? WHERE id=?`,
            now, now, order.Version, id,
        ); err != nil {
            return err
        }
        return s.saveSnapshot(ctx, tx, order)
    }); err != nil {
        return Order{}, err
    }
    return order, nil
}
