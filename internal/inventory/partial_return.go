package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

var (
	ErrInvalidPartialSaleReturn          = errors.New("invalid partial sale return")
	ErrPartialSaleReturnQuantityExceeded = errors.New("partial sale return quantity exceeds issued inventory")
	ErrPartialSaleReturnReplayMismatch   = errors.New("partial sale return replay does not match inventory movement")
)

type PartialSaleReturnLine struct {
	OrderItemID   string
	QuantityMilli int64
}

// ApplyPartialSaleReturnTx restores only the requested quantities that were
// previously issued by a completed sale. The caller owns the transaction so
// inventory compensation can commit atomically with the partial-return ledger,
// payment reversal, order lifecycle, manager audit, and durable return facts.
//
// Idempotency is scoped to one durable return operation. Replaying the same
// return ID with identical item quantities is side-effect free; reusing that ID
// for different inventory facts fails closed.
func (s *Service) ApplyPartialSaleReturnTx(ctx context.Context, tx *sql.Tx, order orders.Order, returnID string, lines []PartialSaleReturnLine) error {
	returnID = strings.TrimSpace(returnID)
	if returnID == "" || order.ID == "" || order.StoreID == "" || len(lines) == 0 {
		return ErrInvalidPartialSaleReturn
	}

	items := make(map[string]orders.Item, len(order.Items))
	for _, item := range order.Items {
		items[item.ID] = item
	}
	seen := make(map[string]struct{}, len(lines))
	now := time.Now().UTC().Format(time.RFC3339Nano)

	for _, input := range lines {
		itemID := strings.TrimSpace(input.OrderItemID)
		if itemID == "" || input.QuantityMilli <= 0 {
			return ErrInvalidPartialSaleReturn
		}
		if _, duplicate := seen[itemID]; duplicate {
			return ErrInvalidPartialSaleReturn
		}
		seen[itemID] = struct{}{}

		item, ok := items[itemID]
		if !ok {
			return ErrInvalidPartialSaleReturn
		}

		movementID := fmt.Sprintf("mov_partial_return:%s:%s", returnID, itemID)
		var existingItemID string
		var existingDelta int64
		err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(order_item_id,''), quantity_delta_milli
			FROM inventory_movements WHERE id=?`, movementID).Scan(&existingItemID, &existingDelta)
		if err == nil {
			if existingItemID != itemID || existingDelta != input.QuantityMilli {
				return ErrPartialSaleReturnReplayMismatch
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		var issuedMilli int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(CASE WHEN quantity_delta_milli < 0 THEN -quantity_delta_milli ELSE quantity_delta_milli END),0)
			FROM inventory_movements
			WHERE order_item_id=? AND movement_type='sale_issue'`, itemID).Scan(&issuedMilli); err != nil {
			return err
		}
		if issuedMilli == 0 {
			// Non-stock-tracked items never produced a sale_issue and therefore
			// must not inflate inventory during a return.
			continue
		}

		var returnedMilli int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(quantity_delta_milli),0)
			FROM inventory_movements
			WHERE order_item_id=? AND movement_type='sale_return'`, itemID).Scan(&returnedMilli); err != nil {
			return err
		}
		if returnedMilli < 0 || returnedMilli+input.QuantityMilli > issuedMilli {
			return ErrPartialSaleReturnQuantityExceeded
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE inventory_balances
			SET on_hand_milli=on_hand_milli+?, version=version+1, updated_at=?
			WHERE store_id=? AND product_id=?`,
			input.QuantityMilli, now, order.StoreID, item.ProductID,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("inventory balance missing for partially returned sale item %s", item.ID)
		}

		var after int64
		if err := tx.QueryRowContext(ctx,
			`SELECT on_hand_milli FROM inventory_balances WHERE store_id=? AND product_id=?`,
			order.StoreID, item.ProductID,
		).Scan(&after); err != nil {
			return err
		}

		refType, refID, orderItemID := "sale_order", order.ID, item.ID
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inventory_movements(
				id,store_id,product_id,movement_type,quantity_delta_milli,
				reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at
			) VALUES(?,?,?,'sale_return',?,?,?,?,?,?,?)`,
			movementID, order.StoreID, item.ProductID, input.QuantityMilli,
			refType, refID, orderItemID, after, now, now,
		); err != nil {
			return err
		}

		movement := Movement{
			ID:                   movementID,
			StoreID:              order.StoreID,
			ProductID:            item.ProductID,
			MovementType:          "sale_return",
			QuantityDeltaMilli:   input.QuantityMilli,
			ReferenceType:        &refType,
			ReferenceID:          &refID,
			OrderItemID:          &orderItemID,
			BalanceAfterMilli:    after,
			OccurredAt:           now,
		}
		if err := appendMovementEventTx(ctx, tx, movement, order.ID); err != nil {
			return err
		}
	}
	return nil
}
