package refunds

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

// DeadLetterSyncHead identifies the exact poisoned outbox fact currently blocking
// one sale ordering key. It is read-only recovery context; it grants no authority.
type DeadLetterSyncHead struct {
	EventID      string `json:"event_id"`
	EventType    string `json:"event_type"`
	AttemptCount int64  `json:"attempt_count"`
	CreatedAt    string `json:"created_at"`
}

// ReconciliationSnapshot is a read-only summary of the local facts that matter
// when a refund is blocked for manual reconciliation. It intentionally exposes
// facts only; it does not decide or apply any corrective action.
type ReconciliationSnapshot struct {
	OrderID                  string              `json:"order_id"`
	OrderStatus              string              `json:"order_status"`
	CapturedPaymentMinor     int64               `json:"captured_payment_minor"`
	ReversedPaymentMinor     int64               `json:"reversed_payment_minor"`
	SaleIssuedQuantityMilli  int64               `json:"sale_issued_quantity_milli"`
	RestoredQuantityMilli    int64               `json:"restored_quantity_milli"`
	PartialReturnOperations  int64               `json:"partial_return_operations"`
	PartialReturnRefundMinor int64               `json:"partial_return_refund_minor"`
	UnpublishedSyncFacts     int64               `json:"unpublished_sync_facts"`
	DeadLetterSyncFacts      int64               `json:"dead_letter_sync_facts"`
	DeadLetterSyncHead       *DeadLetterSyncHead `json:"dead_letter_sync_head,omitempty"`
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

	if snapshot.DeadLetterSyncFacts > 0 {
		head := &DeadLetterSyncHead{}
		if err := s.db.SQL().QueryRowContext(ctx, `
			SELECT id,event_type,attempt_count,created_at
			FROM outbox_events
			WHERE ordering_key=? AND status='dead_letter'
			ORDER BY created_at,id
			LIMIT 1`, orderingKey).Scan(&head.EventID, &head.EventType, &head.AttemptCount, &head.CreatedAt); err != nil {
			return ReconciliationSnapshot{}, err
		}
		snapshot.DeadLetterSyncHead = head
	}

	return snapshot, nil
}
