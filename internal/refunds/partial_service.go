package refunds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
)

type PartialReturnInput struct {
	ReturnID         string
	OrderID          string
	ApprovedByUserID string
	Reason           string
	Lines            []PartialReturnLineInput
}

type partialCapture struct {
	ID       string
	Mode     string
	Currency string
	Amount   int64
	Refunded int64
}

// ReturnPartial atomically plans and records one item-level return, reverses the
// exact cumulative share of captured tenders, restores only the requested stock,
// updates local order audit/version state, and emits a durable item-level return
// fact. When the operation consumes the entire remaining sale, the same transaction
// also advances the order to returned and emits the final sale.returned lifecycle fact.
func (s *Service) ReturnPartial(ctx context.Context, input PartialReturnInput) (orders.Order, PartialReturnPlan, error) {
	input.ReturnID = strings.TrimSpace(input.ReturnID)
	input.OrderID = strings.TrimSpace(input.OrderID)
	input.ApprovedByUserID = strings.TrimSpace(input.ApprovedByUserID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ReturnID == "" || input.OrderID == "" || input.ApprovedByUserID == "" || input.Reason == "" || len(input.Lines) == 0 {
		return orders.Order{}, PartialReturnPlan{}, ErrInvalidPartialReturn
	}

	order, err := s.orders.Get(ctx, input.OrderID)
	if err != nil {
		return orders.Order{}, PartialReturnPlan{}, err
	}

	var resultOrder orders.Order
	var resultPlan PartialReturnPlan
	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if replayed, replayOrder, replayPlan, err := loadPartialReturnReplay(ctx, tx, order, input); err != nil {
			return err
		} else if replayed {
			resultOrder, resultPlan = replayOrder, replayPlan
			return nil
		}

		history, err := LoadPartialReturnHistoryTx(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		plan, err := PlanPartialReturn(order, input.Lines, history)
		if err != nil {
			return err
		}

		ledgerLines := make([]PartialReturnLedgerLine, 0, len(plan.Lines))
		inventoryLines := make([]inventory.PartialSaleReturnLine, 0, len(plan.Lines))
		outboxLines := make([]outbox.SalePartialReturnedLine, 0, len(plan.Lines))
		for _, line := range plan.Lines {
			ledgerLines = append(ledgerLines, PartialReturnLedgerLine{
				OrderItemID: line.OrderItemID,
				QuantityMilli: line.QuantityMilli,
				RefundMinor: line.RefundMinor,
			})
			inventoryLines = append(inventoryLines, inventory.PartialSaleReturnLine{
				OrderItemID: line.OrderItemID,
				QuantityMilli: line.QuantityMilli,
			})
			outboxLines = append(outboxLines, outbox.SalePartialReturnedLine{
				OrderItemID: line.OrderItemID,
				QuantityMilli: line.QuantityMilli,
				RefundMinor: line.RefundMinor,
			})
		}
		if _, err := AppendPartialReturnTx(ctx, tx, PartialReturnLedgerRecord{
			ID: input.ReturnID, OrderID: order.ID, ApprovedByUserID: input.ApprovedByUserID,
			Reason: input.Reason, RefundMinor: plan.RefundMinor, Lines: ledgerLines,
		}); err != nil {
			return err
		}

		var priorRefundMinor int64
		for _, h := range history {
			priorRefundMinor += h.RefundMinor
		}
		if err := s.reversePartialTendersTx(ctx, tx, order, input.ReturnID, priorRefundMinor+plan.RefundMinor); err != nil {
			return err
		}
		if err := s.inventory.ApplyPartialSaleReturnTx(ctx, tx, order, input.ReturnID, inventoryLines); err != nil {
			return err
		}

		updated, err := s.orders.ApplyPartialReturnStateTx(ctx, tx, order, input.ApprovedByUserID, input.Reason, plan.FullRemaining)
		if err != nil {
			return err
		}
		if err := s.outbox.ApplySalePartialReturnedTx(ctx, tx, updated, input.ReturnID, plan.RefundMinor, outboxLines, input.ApprovedByUserID, input.Reason); err != nil {
			return err
		}
		if plan.FullRemaining {
			if err := s.outbox.ApplySaleReturnedTx(ctx, tx, updated, input.ApprovedByUserID, input.Reason); err != nil {
				return err
			}
		}
		resultOrder, resultPlan = updated, plan
		return nil
	})
	if err != nil {
		return orders.Order{}, PartialReturnPlan{}, err
	}
	return resultOrder, resultPlan, nil
}

func (s *Service) reversePartialTendersTx(ctx context.Context, tx *sql.Tx, order orders.Order, returnID string, cumulativeRefundMinor int64) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id,p.mode,p.currency,p.amount_minor,
		       COALESCE((SELECT SUM(r.amount_minor) FROM payments r
		                 WHERE r.order_id=p.order_id AND r.direction='out'
		                   AND r.status IN ('captured','refunded') AND r.reference=p.id),0)
		FROM payments p
		WHERE p.order_id=? AND p.direction='in' AND p.status='captured'
		ORDER BY p.created_at,p.id`, order.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	captures := []partialCapture{}
	var totalCaptured int64
	for rows.Next() {
		var capture partialCapture
		if err := rows.Scan(&capture.ID, &capture.Mode, &capture.Currency, &capture.Amount, &capture.Refunded); err != nil {
			return err
		}
		if capture.Refunded < 0 || capture.Refunded > capture.Amount {
			return ErrExistingReversal
		}
		totalCaptured += capture.Amount
		captures = append(captures, capture)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if cumulativeRefundMinor < 0 || cumulativeRefundMinor > totalCaptured || (cumulativeRefundMinor > 0 && totalCaptured == 0) {
		return ErrInvalidPartialReturn
	}
	if cumulativeRefundMinor == 0 {
		return nil
	}

	var allocatedTarget int64
	for i, capture := range captures {
		target := proportionalFloor(capture.Amount, cumulativeRefundMinor, totalCaptured)
		if i == len(captures)-1 {
			target = cumulativeRefundMinor - allocatedTarget
		}
		allocatedTarget += target
		if capture.Refunded > target {
			return ErrExistingReversal
		}
		delta := target - capture.Refunded
		if delta == 0 {
			continue
		}
		reference := capture.ID
		if _, _, err := s.payments.CreateRefundTx(ctx, tx, order.ID, payments.CreateInput{
			ClientPaymentID: fmt.Sprintf("partial-refund:%s:%s", returnID, capture.ID),
			Mode: capture.Mode, Direction: "out", AmountMinor: delta, Currency: capture.Currency,
			Status: "refunded", Reference: &reference,
		}); err != nil {
			return err
		}
	}
	return nil
}

func loadPartialReturnReplay(ctx context.Context, tx *sql.Tx, order orders.Order, input PartialReturnInput) (bool, orders.Order, PartialReturnPlan, error) {
	var orderID, approver, reason string
	var refundMinor int64
	err := tx.QueryRowContext(ctx, `
		SELECT order_id,approved_by_user_id,reason,refund_minor
		FROM pos_partial_returns WHERE id=?`, input.ReturnID).Scan(&orderID, &approver, &reason, &refundMinor)
	if errors.Is(err, sql.ErrNoRows) {
		return false, orders.Order{}, PartialReturnPlan{}, nil
	}
	if err != nil {
		return false, orders.Order{}, PartialReturnPlan{}, err
	}
	if orderID != input.OrderID || approver != input.ApprovedByUserID || reason != input.Reason {
		return false, orders.Order{}, PartialReturnPlan{}, ErrPartialReturnReplayMismatch
	}

	requested := make(map[string]int64, len(input.Lines))
	for _, line := range input.Lines {
		itemID := strings.TrimSpace(line.OrderItemID)
		if itemID == "" || line.QuantityMilli <= 0 {
			return false, orders.Order{}, PartialReturnPlan{}, ErrInvalidPartialReturn
		}
		if _, exists := requested[itemID]; exists {
			return false, orders.Order{}, PartialReturnPlan{}, ErrInvalidPartialReturn
		}
		requested[itemID] = line.QuantityMilli
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT order_item_id,quantity_milli,refund_minor
		FROM pos_partial_return_lines WHERE return_id=?`, input.ReturnID)
	if err != nil {
		return false, orders.Order{}, PartialReturnPlan{}, err
	}
	defer rows.Close()
	plan := PartialReturnPlan{RefundMinor: refundMinor}
	for rows.Next() {
		var line PartialReturnLinePlan
		if err := rows.Scan(&line.OrderItemID, &line.QuantityMilli, &line.RefundMinor); err != nil {
			return false, orders.Order{}, PartialReturnPlan{}, err
		}
		if requested[line.OrderItemID] != line.QuantityMilli {
			return false, orders.Order{}, PartialReturnPlan{}, ErrPartialReturnReplayMismatch
		}
		delete(requested, line.OrderItemID)
		plan.Lines = append(plan.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return false, orders.Order{}, PartialReturnPlan{}, err
	}
	if len(requested) != 0 {
		return false, orders.Order{}, PartialReturnPlan{}, ErrPartialReturnReplayMismatch
	}

	current := order
	if err := tx.QueryRowContext(ctx, `SELECT status,version,updated_at FROM sales_orders WHERE id=?`, order.ID).Scan(&current.Status, &current.Version, &current.UpdatedAt); err != nil {
		return false, orders.Order{}, PartialReturnPlan{}, err
	}
	plan.FullRemaining = current.Status == "returned"
	return true, current, plan, nil
}
