package receipts

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func applyReceiptIssuedTx(ctx context.Context, tx *sql.Tx, receipt Receipt) error {
	payload, err := json.Marshal(map[string]any{"receipt": receipt})
	if err != nil {
		return fmt.Errorf("marshal receipt issued event: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"source":      "pos_service",
		"order_id":    receipt.OrderID,
		"store_id":    receipt.StoreID,
		"terminal_id": receipt.TerminalID,
		"occurred_at": receipt.IssuedAt,
	})
	if err != nil {
		return fmt.Errorf("marshal receipt issued metadata: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
			ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
		) VALUES(?,?,?,1,'receipt.issued',1,?,?,?,'pending',0,?,?)
		ON CONFLICT(aggregate_type,aggregate_id,aggregate_version,event_type) DO NOTHING`,
		"evt_receipt_"+receipt.ID, "receipt", receipt.ID, "sales_order:"+receipt.OrderID,
		string(payload), string(metadata), now, now,
	)
	return err
}
