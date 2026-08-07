package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

func appendMovementEventTx(ctx context.Context, tx *sql.Tx, movement Movement, orderID string) error {
	payload, err := json.Marshal(map[string]any{"movement": movement})
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"source":      "pos_service",
		"store_id":    movement.StoreID,
		"product_id":  movement.ProductID,
		"occurred_at": movement.OccurredAt,
	})
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
			ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
		) VALUES(?,?,?,1,'inventory.movement.recorded',1,?,?,?,'pending',0,?,?)
		ON CONFLICT(aggregate_type,aggregate_id,aggregate_version,event_type) DO NOTHING`,
		"evt_inventory_"+movement.ID,
		"inventory_movement",
		movement.ID,
		"sales_order:"+orderID,
		string(payload),
		string(metadata),
		now,
		now,
	)
	return err
}
