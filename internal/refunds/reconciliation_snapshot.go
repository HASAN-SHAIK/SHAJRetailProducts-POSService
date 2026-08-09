package refunds

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

// ReconciliationSnapshot is a read-only summary of the local facts that matter
// when a refund is blocked for manual reconciliation. It intentionally exposes
// facts only; it does not decide or apply any corrective action.
type ReconciliationSnapshot struct {
	OrderID                  string `json:"order_id"`
	OrderStatus              string `json:"order_status"`
	CapturedPaymentMinor     int64  `json:"captured_payment_minor"`
	ReversedPaymentMinor     int64  `json:"reversed_payment_minor"`
	SaleIssuedQuantityMilli  int64  `json:"sale_issued_quantity_milli"`
	RestoredQuantityMilli    int64  `json:"restored_quantity_milli"`
	PartialReturnOperations  int64  `json:"partial_return_operations"`
	PartialReturnRefundMinor int64  `json:"partial_return_refund_minor"`
	UnpublishedSyncFacts     int64  `json:"unpublished_sync_facts"`
	DeadLetterSyncFacts      int64  `json:"dead_letter_sync_facts"`
}

// GetReconciliationSnapshot reads payment, inventory, partial-return ledger and
// outbound sync facts for one sale without mutating any POS state.
func (s *Service) GetReconciliationSnapshot(ctx context.Context, orderID string) (ReconciliationSnapshot, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return ReconciliationSnapshot{}, ErrInvalidPartialReturn
	}

	snapshot := ReconciliationSnapshot{OrderID: orderID}
	if err := s.db.SQL().QueryRowContext(ctx, `SELECT status FROM sales_orders WHERE id=?`, orderID).Scan(&snapshot.OrderStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReconciliationSnapshot{}, orders.ErrNotFound
		}
		return ReconciliationSnapshot{}, err
	}

	if err := s.db.SQL().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount_minor),0)
		FROM payments
		WHERE order_id=? AND direction='in' AND status='captured'`, orderID).Scan(&snapshot.CapturedPaymentMinor); err != nil {
		return ReconciliationSnapshot{}, err
	}

	if err := s.db.SQL().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount_minor),0)
		FROM payments
		WHERE order_id=? AND direction='out' AND status IN ('captured','refunded')`, orderID).Scan(&snapshot.ReversedPaymentMinor); err != nil {
		return ReconciliationSnapshot{}, err
	}

	if err := s.db.SQL().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(-quantity_delta_milli),0)
		FROM inventory_movements
		WHERE reference_type='sale_order' AND reference_id=? AND movement_type='sale_issue'`, orderID).Scan(&snapshot.SaleIssuedQuantityMilli); err != nil {
		return ReconciliationSnapshot{}, err
	}

	if err := s.db.SQL().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(quantity_delta_milli),0)
		FROM inventory_movements
		WHERE reference_type='sale_order' AND reference_id=? AND movement_type='sale_return'`, orderID).Scan(&snapshot.RestoredQuantityMilli); err != nil {
		return ReconciliationSnapshot{}, err
	}

	if err := s.db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*),COALESCE(SUM(refund_minor),0)
		FROM pos_partial_returns
		WHERE order_id=?`, orderID).Scan(&snapshot.PartialReturnOperations, &snapshot.PartialReturnRefundMinor); err != nil {
		return ReconciliationSnapshot{}, err
	}

	orderingKey := "sales_order:" + orderID
	if err := s.db.SQL().QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status <> 'published' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status = 'dead_letter' THEN 1 ELSE 0 END),0)
		FROM outbox_events
		WHERE ordering_key=?`, orderingKey).Scan(&snapshot.UnpublishedSyncFacts, &snapshot.DeadLetterSyncFacts); err != nil {
		return ReconciliationSnapshot{}, err
	}

	return snapshot, nil
}
