package customer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func applyCustomerChangedTx(ctx context.Context, tx *sql.Tx, customer Customer) error {
	payload, err := json.Marshal(map[string]any{"customer": customer})
	if err != nil {
		return fmt.Errorf("marshal customer changed event: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"source":      "pos_service",
		"occurred_at": customer.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("marshal customer changed metadata: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	eventID := fmt.Sprintf("evt_customer_%s_v%d", customer.ID, customer.LocalVersion)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
			ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
		) VALUES(?,?,?,?,'customer.changed',1,?,?,?,'pending',0,?,?)
		ON CONFLICT(aggregate_type,aggregate_id,aggregate_version,event_type) DO NOTHING`,
		eventID, "customer", customer.ID, customer.LocalVersion, "customer:"+customer.ID,
		string(payload), string(metadata), now, now,
	)
	return err
}
