package refunds

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

// ListPartialReturns returns durable item-level return operations for one order
// in deterministic creation order. It is read-only and does not alter refund,
// inventory, approval, or sync state.
func (s *Service) ListPartialReturns(ctx context.Context, orderID string) ([]PartialReturnLedgerRecord, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, ErrInvalidPartialReturn
	}

	var exists int
	if err := s.db.SQL().QueryRowContext(ctx, `SELECT 1 FROM sales_orders WHERE id=?`, orderID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, orders.ErrNotFound
		}
		return nil, err
	}

	rows, err := s.db.SQL().QueryContext(ctx, `
		SELECT id,order_id,approved_by_user_id,reason,refund_minor,created_at
		FROM pos_partial_returns
		WHERE order_id=?
		ORDER BY created_at,id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]PartialReturnLedgerRecord, 0)
	for rows.Next() {
		var record PartialReturnLedgerRecord
		if err := rows.Scan(
			&record.ID,
			&record.OrderID,
			&record.ApprovedByUserID,
			&record.Reason,
			&record.RefundMinor,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range records {
		lineRows, err := s.db.SQL().QueryContext(ctx, `
			SELECT order_item_id,quantity_milli,refund_minor
			FROM pos_partial_return_lines
			WHERE return_id=?
			ORDER BY order_item_id`, records[i].ID)
		if err != nil {
			return nil, err
		}
		for lineRows.Next() {
			var line PartialReturnLedgerLine
			if err := lineRows.Scan(&line.OrderItemID, &line.QuantityMilli, &line.RefundMinor); err != nil {
				lineRows.Close()
				return nil, err
			}
			records[i].Lines = append(records[i].Lines, line)
		}
		if err := lineRows.Err(); err != nil {
			lineRows.Close()
			return nil, err
		}
		if err := lineRows.Close(); err != nil {
			return nil, err
		}
	}

	return records, nil
}
