package outbox

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
)

type PaymentRecordedPayload struct {
    Payment payments.Payment `json:"payment"`
    Summary payments.Summary `json:"summary"`
}

func (s *Service) ApplyPaymentRecordedTx(ctx context.Context, tx *sql.Tx, payment payments.Payment, summary payments.Summary) error {
    payload, err := json.Marshal(PaymentRecordedPayload{Payment: payment, Summary: summary})
    if err != nil { return fmt.Errorf("marshal payment recorded event: %w", err) }
    metadata, err := json.Marshal(map[string]any{
        "source": "pos_service",
        "order_id": payment.OrderID,
        "occurred_at": payment.CreatedAt,
    })
    if err != nil { return err }

    now := time.Now().UTC().Format(time.RFC3339Nano)
    eventID := "evt_payment_" + payment.ID
    _, err = tx.ExecContext(ctx, `
        INSERT INTO outbox_events(
            id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
            ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
        ) VALUES(?,?,?,1,'payment.recorded',1,?,?,?,'pending',0,?,?)
        ON CONFLICT(aggregate_type,aggregate_id,aggregate_version,event_type) DO NOTHING`,
        eventID, "payment", payment.ID, "sales_order:"+payment.OrderID,
        string(payload), string(metadata), now, now,
    )
    return err
}
