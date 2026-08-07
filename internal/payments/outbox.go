package payments

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "time"
)

type paymentRecordedPayload struct {
    Payment Payment `json:"payment"`
    Summary Summary `json:"summary"`
}

func appendRecordedEventTx(ctx context.Context, tx *sql.Tx, payment Payment, summary Summary) error {
    payload, err := json.Marshal(paymentRecordedPayload{Payment: payment, Summary: summary})
    if err != nil { return fmt.Errorf("marshal payment recorded event: %w", err) }
    metadata, err := json.Marshal(map[string]any{
        "source": "pos_service",
        "order_id": payment.OrderID,
        "occurred_at": payment.CreatedAt,
    })
    if err != nil { return err }

    now := time.Now().UTC().Format(time.RFC3339Nano)
    _, err = tx.ExecContext(ctx, `
        INSERT INTO outbox_events(
            id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
            ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
        ) VALUES(?,?,?,1,'payment.recorded',1,?,?,?,'pending',0,?,?)
        ON CONFLICT(aggregate_type,aggregate_id,aggregate_version,event_type) DO NOTHING`,
        "evt_payment_"+payment.ID, "payment", payment.ID, "sales_order:"+payment.OrderID,
        string(payload), string(metadata), now, now,
    )
    return err
}
