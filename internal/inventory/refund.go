package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

// ApplySaleReturnTx restores only inventory that was previously issued for the
// completed sale. It is intentionally transaction-scoped so the future refund
// workflow can commit inventory, payment reversal, order state, audit, and
// outbox facts atomically.
//
// The operation is idempotent per order item. Items that never produced a
// sale_issue movement (for example non-stock-tracked products) are skipped and
// therefore cannot inflate on-hand stock during a refund.
func (s *Service) ApplySaleReturnTx(ctx context.Context, tx *sql.Tx, order orders.Order) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range order.Items {
		var issued int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM inventory_movements WHERE order_item_id=? AND movement_type='sale_issue'`,
			item.ID,
		).Scan(&issued); err != nil {
			return err
		}
		if issued == 0 {
			continue
		}

		var alreadyReturned int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM inventory_movements WHERE order_item_id=? AND movement_type='sale_return'`,
			item.ID,
		).Scan(&alreadyReturned); err != nil {
			return err
		}
		if alreadyReturned > 0 {
			continue
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE inventory_balances
			SET on_hand_milli=on_hand_milli+?, version=version+1, updated_at=?
			WHERE store_id=? AND product_id=?`,
			item.QuantityMilli, now, order.StoreID, item.ProductID,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("inventory balance missing for returned sale item %s", item.ID)
		}

		var after int64
		if err := tx.QueryRowContext(ctx,
			`SELECT on_hand_milli FROM inventory_balances WHERE store_id=? AND product_id=?`,
			order.StoreID, item.ProductID,
		).Scan(&after); err != nil {
			return err
		}

		refType, refID, orderItemID := "sale_order", order.ID, item.ID
		movementID := fmt.Sprintf("mov_return_%d_%d", time.Now().UTC().UnixNano(), item.LineNo)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inventory_movements(
				id,store_id,product_id,movement_type,quantity_delta_milli,
				reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at
			) VALUES(?,?,?,'sale_return',?,?,?,?,?,?,?)`,
			movementID, order.StoreID, item.ProductID, item.QuantityMilli,
			refType, refID, orderItemID, after, now, now,
		); err != nil {
			return err
		}

		movement := Movement{
			ID: movementID,
			StoreID: order.StoreID,
			ProductID: item.ProductID,
			MovementType: "sale_return",
			QuantityDeltaMilli: item.QuantityMilli,
			ReferenceType: &refType,
			ReferenceID: &refID,
			OrderItemID: &orderItemID,
			BalanceAfterMilli: after,
			OccurredAt: now,
		}
		if err := appendMovementEventTx(ctx, tx, movement, order.ID); err != nil {
			return err
		}
	}
	return nil
}
