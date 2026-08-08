package payments

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "time"
)

// CreateRefundTx records a compensating payment inside the caller's transaction.
// It is intentionally restricted to refund semantics so the future full-sale
// refund workflow can atomically combine money reversal, inventory restoration,
// order state, audit metadata and durable outbox facts.
func (s *Service) CreateRefundTx(ctx context.Context, tx *sql.Tx, orderID string, input CreateInput) (Payment, Summary, error) {
    input.ClientPaymentID = strings.TrimSpace(input.ClientPaymentID)
    input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
    input.Direction = strings.ToLower(strings.TrimSpace(input.Direction))
    input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
    input.Status = strings.ToLower(strings.TrimSpace(input.Status))

    if input.ClientPaymentID == "" || input.AmountMinor <= 0 || !validMode(input.Mode) {
        return Payment{}, Summary{}, ErrInvalidPayment
    }
    if input.Direction == "" { input.Direction = "out" }
    if input.Direction != "out" { return Payment{}, Summary{}, ErrInvalidPayment }
    if input.Status == "" { input.Status = "refunded" }
    if input.Status != "refunded" { return Payment{}, Summary{}, ErrInvalidPayment }
    if len(input.ProviderPayload) > 0 && !json.Valid(input.ProviderPayload) {
        return Payment{}, Summary{}, ErrInvalidPayment
    }

    var orderCurrency, orderStatus string
    var totalMinor int64
    if err := tx.QueryRowContext(ctx, `SELECT currency,total_minor,status FROM sales_orders WHERE id=?`, orderID).Scan(&orderCurrency, &totalMinor, &orderStatus); err != nil {
        if errors.Is(err, sql.ErrNoRows) { return Payment{}, Summary{}, ErrOrderNotFound }
        return Payment{}, Summary{}, err
    }
    if orderStatus == "cancelled" || orderStatus == "returned" {
        return Payment{}, Summary{}, ErrInvalidPayment
    }
    if input.Currency == "" { input.Currency = orderCurrency }
    if input.Currency != orderCurrency { return Payment{}, Summary{}, ErrInvalidPayment }

    existing, err := getByClientIDTx(ctx, tx, input.ClientPaymentID)
    if err == nil {
        if existing.OrderID != orderID || !paymentMatchesInput(existing, input) {
            return Payment{}, Summary{}, ErrInvalidPayment
        }
        summary, err := paymentSummaryTx(ctx, tx, orderID, totalMinor)
        return existing, summary, err
    }
    if !errors.Is(err, ErrNotFound) { return Payment{}, Summary{}, err }

    // Never compensate more than the net amount currently captured for the sale.
    var paidMinor int64
    if err := tx.QueryRowContext(ctx, paymentTotalSQL, orderID).Scan(&paidMinor); err != nil {
        return Payment{}, Summary{}, err
    }
    if paidMinor <= 0 || input.AmountMinor > paidMinor {
        return Payment{}, Summary{}, ErrInvalidPayment
    }

    now := time.Now().UTC().Format(time.RFC3339Nano)
    id, err := newID("pay")
    if err != nil { return Payment{}, Summary{}, err }
    var providerPayload any
    if len(input.ProviderPayload) > 0 { providerPayload = string(input.ProviderPayload) }
    if _, err := tx.ExecContext(ctx, `INSERT INTO payments(id,order_id,client_payment_id,mode,direction,amount_minor,currency,status,reference,provider,provider_payload_json,recorded_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
        id, orderID, input.ClientPaymentID, input.Mode, input.Direction, input.AmountMinor, input.Currency, input.Status, input.Reference, input.Provider, providerPayload, input.RecordedBy, now, now); err != nil {
        return Payment{}, Summary{}, fmt.Errorf("insert refund payment: %w", err)
    }

    created := Payment{
        ID: id, OrderID: orderID, ClientPaymentID: input.ClientPaymentID,
        Mode: input.Mode, Direction: input.Direction, AmountMinor: input.AmountMinor,
        Currency: input.Currency, Status: input.Status, Reference: input.Reference,
        Provider: input.Provider, ProviderPayload: input.ProviderPayload,
        RecordedBy: input.RecordedBy, CreatedAt: now, UpdatedAt: now,
    }
    snapshot, _ := json.Marshal(created)
    if _, err := tx.ExecContext(ctx, `INSERT INTO payment_snapshots(payment_id,version,snapshot_json,created_at) VALUES(?,?,?,?)`, id, 1, string(snapshot), now); err != nil {
        return Payment{}, Summary{}, err
    }

    summary, err := recalcOrderPaymentState(ctx, tx, orderID, totalMinor)
    if err != nil { return Payment{}, Summary{}, err }
    if s.recordedHook != nil {
        if err := s.recordedHook(ctx, tx, created, summary); err != nil {
            return Payment{}, Summary{}, err
        }
    }
    return created, summary, nil
}
