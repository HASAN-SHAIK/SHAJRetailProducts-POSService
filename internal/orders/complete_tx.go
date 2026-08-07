package orders

import (
    "context"
    "database/sql"
    "time"
)

type CompletionHook func(context.Context, *sql.Tx, Order) error

// CompleteWith completes an order and runs dependent local effects in the same
// SQLite transaction. Inventory, receipts and outbox modules extend this hook
// without making sale completion depend on the central server.
func (s *Service) CompleteWith(ctx context.Context, id string, hook CompletionHook) (Order, error) {
    order, err := s.Get(ctx, id)
    if err != nil { return Order{}, err }
    if order.CompletedAt != nil || order.Status == "cancelled" || order.Status == "returned" {
        return Order{}, ErrAlreadyComplete
    }

    now := time.Now().UTC().Format(time.RFC3339Nano)
    order.CompletedAt = &now
    order.UpdatedAt = now
    order.Version++
    if order.Status == "draft" { order.Status = "confirmed" }

    if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
        if hook != nil {
            if err := hook(ctx, tx, order); err != nil { return err }
        }
        if _, err := tx.ExecContext(ctx, `UPDATE sales_orders SET status=?, completed_at=?, updated_at=?, version=? WHERE id=?`, order.Status, now, now, order.Version, id); err != nil {
            return err
        }
        return s.saveSnapshot(ctx, tx, order)
    }); err != nil { return Order{}, err }
    return order, nil
}
