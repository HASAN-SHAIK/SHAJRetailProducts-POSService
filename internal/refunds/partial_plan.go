package refunds

import (
	"errors"
	"math/big"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

var (
	ErrInvalidPartialReturn = errors.New("invalid partial return")
	ErrReturnQuantityExceeded = errors.New("return quantity exceeds remaining sale quantity")
)

type PartialReturnLineInput struct {
	OrderItemID  string
	QuantityMilli int64
}

type PartialReturnHistory struct {
	QuantityMilli int64
	RefundMinor   int64
}

type PartialReturnLinePlan struct {
	OrderItemID             string
	QuantityMilli           int64
	RefundMinor             int64
	CumulativeQuantityMilli int64
	CumulativeRefundMinor   int64
}

type PartialReturnPlan struct {
	Lines         []PartialReturnLinePlan
	RefundMinor   int64
	FullRemaining bool
}

// PlanPartialReturn validates an item-level return against the immutable sale
// lines and computes the exact incremental refund value for this operation.
//
// Refund allocation uses cumulative proportional rounding. That means repeated
// partial returns of the same line cannot lose or create minor currency units:
// when the final remaining quantity is returned, the cumulative refund equals
// the original line total exactly.
//
// history must represent durable prior return facts for each order item. This
// planner is intentionally side-effect free; the transaction/ledger layer that
// persists history is a separate slice.
func PlanPartialReturn(order orders.Order, requested []PartialReturnLineInput, history map[string]PartialReturnHistory) (PartialReturnPlan, error) {
	if order.CompletedAt == nil || order.Status == "cancelled" || order.Status == "returned" {
		return PartialReturnPlan{}, ErrInvalidPartialReturn
	}
	if len(requested) == 0 {
		return PartialReturnPlan{}, ErrInvalidPartialReturn
	}

	items := make(map[string]orders.Item, len(order.Items))
	for _, item := range order.Items {
		items[item.ID] = item
	}
	seen := make(map[string]struct{}, len(requested))
	requestedByItem := make(map[string]int64, len(requested))
	plan := PartialReturnPlan{Lines: make([]PartialReturnLinePlan, 0, len(requested))}

	for _, input := range requested {
		itemID := strings.TrimSpace(input.OrderItemID)
		if itemID == "" || input.QuantityMilli <= 0 {
			return PartialReturnPlan{}, ErrInvalidPartialReturn
		}
		if _, duplicate := seen[itemID]; duplicate {
			return PartialReturnPlan{}, ErrInvalidPartialReturn
		}
		seen[itemID] = struct{}{}

		item, ok := items[itemID]
		if !ok || item.QuantityMilli <= 0 || item.LineTotalMinor < 0 {
			return PartialReturnPlan{}, ErrInvalidPartialReturn
		}
		prior := history[itemID]
		if prior.QuantityMilli < 0 || prior.RefundMinor < 0 || prior.QuantityMilli > item.QuantityMilli || prior.RefundMinor > item.LineTotalMinor {
			return PartialReturnPlan{}, ErrInvalidPartialReturn
		}

		cumulativeQty := prior.QuantityMilli + input.QuantityMilli
		if cumulativeQty > item.QuantityMilli {
			return PartialReturnPlan{}, ErrReturnQuantityExceeded
		}
		targetCumulativeRefund := proportionalFloor(item.LineTotalMinor, cumulativeQty, item.QuantityMilli)
		if cumulativeQty == item.QuantityMilli {
			targetCumulativeRefund = item.LineTotalMinor
		}
		if prior.RefundMinor > targetCumulativeRefund {
			return PartialReturnPlan{}, ErrInvalidPartialReturn
		}
		incrementalRefund := targetCumulativeRefund - prior.RefundMinor

		plan.Lines = append(plan.Lines, PartialReturnLinePlan{
			OrderItemID: itemID,
			QuantityMilli: input.QuantityMilli,
			RefundMinor: incrementalRefund,
			CumulativeQuantityMilli: cumulativeQty,
			CumulativeRefundMinor: targetCumulativeRefund,
		})
		plan.RefundMinor += incrementalRefund
		requestedByItem[itemID] = input.QuantityMilli
	}

	plan.FullRemaining = true
	for _, item := range order.Items {
		prior := history[item.ID]
		if prior.QuantityMilli < 0 || prior.QuantityMilli > item.QuantityMilli {
			return PartialReturnPlan{}, ErrInvalidPartialReturn
		}
		if prior.QuantityMilli+requestedByItem[item.ID] != item.QuantityMilli {
			plan.FullRemaining = false
			break
		}
	}
	return plan, nil
}

func proportionalFloor(total, numerator, denominator int64) int64 {
	if total <= 0 || numerator <= 0 || denominator <= 0 {
		return 0
	}
	var a, b, d big.Int
	a.SetInt64(total)
	b.SetInt64(numerator)
	d.SetInt64(denominator)
	a.Mul(&a, &b)
	a.Quo(&a, &d)
	return a.Int64()
}
