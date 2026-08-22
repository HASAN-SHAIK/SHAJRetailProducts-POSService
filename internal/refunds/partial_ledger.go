package refunds

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var ErrPartialReturnReplayMismatch = errors.New("partial return replay does not match durable ledger")

type PartialReturnLedgerLine struct {
	OrderItemID   string
	QuantityMilli int64
	RefundMinor   int64
}

type PartialReturnLedgerRecord struct {
	ID                string
	OrderID           string
	InitiatedByUserID string
	ApprovedByUserID  string
	Reason            string
	RefundMinor       int64
	Lines             []PartialReturnLedgerLine
	CreatedAt         string
}

func LoadPartialReturnHistoryTx(ctx context.Context, tx *sql.Tx, orderID string) (map[string]PartialReturnHistory, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" { return nil, ErrInvalidPartialReturn }
	rows, err := tx.QueryContext(ctx, `
		SELECT order_item_id, COALESCE(SUM(quantity_milli),0), COALESCE(SUM(refund_minor),0)
		FROM pos_partial_return_lines WHERE order_id=? GROUP BY order_item_id`, orderID)
	if err != nil { return nil, err }
	defer rows.Close()
	history := map[string]PartialReturnHistory{}
	for rows.Next() {
		var itemID string
		var h PartialReturnHistory
		if err := rows.Scan(&itemID, &h.QuantityMilli, &h.RefundMinor); err != nil { return nil, err }
		history[itemID] = h
	}
	if err := rows.Err(); err != nil { return nil, err }
	return history, nil
}

func AppendPartialReturnTx(ctx context.Context, tx *sql.Tx, record PartialReturnLedgerRecord) (bool, error) {
	record.ID = strings.TrimSpace(record.ID)
	record.OrderID = strings.TrimSpace(record.OrderID)
	record.InitiatedByUserID = strings.TrimSpace(record.InitiatedByUserID)
	record.ApprovedByUserID = strings.TrimSpace(record.ApprovedByUserID)
	if record.InitiatedByUserID == "" { record.InitiatedByUserID = record.ApprovedByUserID }
	record.Reason = strings.TrimSpace(record.Reason)
	if record.ID == "" || record.OrderID == "" || record.InitiatedByUserID == "" || record.ApprovedByUserID == "" || record.Reason == "" || record.RefundMinor < 0 || len(record.Lines) == 0 {
		return false, ErrInvalidPartialReturn
	}
	if record.CreatedAt == "" { record.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano) }

	seen := make(map[string]struct{}, len(record.Lines))
	var lineRefundTotal int64
	for i := range record.Lines {
		record.Lines[i].OrderItemID = strings.TrimSpace(record.Lines[i].OrderItemID)
		line := record.Lines[i]
		if line.OrderItemID == "" || line.QuantityMilli <= 0 || line.RefundMinor < 0 { return false, ErrInvalidPartialReturn }
		if _, ok := seen[line.OrderItemID]; ok { return false, ErrInvalidPartialReturn }
		seen[line.OrderItemID] = struct{}{}
		lineRefundTotal += line.RefundMinor
	}
	if lineRefundTotal != record.RefundMinor { return false, ErrInvalidPartialReturn }

	result, err := tx.ExecContext(ctx, `
		INSERT INTO pos_partial_returns(id,order_id,initiated_by_user_id,approved_by_user_id,reason,refund_minor,created_at)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		record.ID, record.OrderID, record.InitiatedByUserID, record.ApprovedByUserID, record.Reason, record.RefundMinor, record.CreatedAt)
	if err != nil { return false, err }
	affected, err := result.RowsAffected()
	if err != nil { return false, err }
	if affected == 0 {
		match, err := partialReturnReplayMatches(ctx, tx, record)
		if err != nil { return false, err }
		if !match { return false, ErrPartialReturnReplayMismatch }
		return false, nil
	}
	for _, line := range record.Lines {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pos_partial_return_lines(return_id,order_id,order_item_id,quantity_milli,refund_minor,created_at)
			VALUES(?,?,?,?,?,?)`, record.ID, record.OrderID, line.OrderItemID, line.QuantityMilli, line.RefundMinor, record.CreatedAt); err != nil { return false, err }
	}
	return true, nil
}

func partialReturnReplayMatches(ctx context.Context, tx *sql.Tx, record PartialReturnLedgerRecord) (bool, error) {
	var orderID, initiator, approver, reason string
	var refundMinor int64
	if err := tx.QueryRowContext(ctx, `
		SELECT order_id,COALESCE(initiated_by_user_id,approved_by_user_id),approved_by_user_id,reason,refund_minor
		FROM pos_partial_returns WHERE id=?`, record.ID).Scan(&orderID, &initiator, &approver, &reason, &refundMinor); err != nil { return false, err }
	if orderID != record.OrderID || initiator != record.InitiatedByUserID || approver != record.ApprovedByUserID || reason != record.Reason || refundMinor != record.RefundMinor { return false, nil }
	rows, err := tx.QueryContext(ctx, `SELECT order_item_id,quantity_milli,refund_minor FROM pos_partial_return_lines WHERE return_id=?`, record.ID)
	if err != nil { return false, err }
	defer rows.Close()
	existing := make(map[string]PartialReturnLedgerLine, len(record.Lines))
	for rows.Next() {
		var line PartialReturnLedgerLine
		if err := rows.Scan(&line.OrderItemID, &line.QuantityMilli, &line.RefundMinor); err != nil { return false, err }
		existing[line.OrderItemID] = line
	}
	if err := rows.Err(); err != nil { return false, err }
	if len(existing) != len(record.Lines) { return false, nil }
	for _, line := range record.Lines {
		got, ok := existing[line.OrderItemID]
		if !ok || got.QuantityMilli != line.QuantityMilli || got.RefundMinor != line.RefundMinor { return false, nil }
	}
	return true, nil
}
