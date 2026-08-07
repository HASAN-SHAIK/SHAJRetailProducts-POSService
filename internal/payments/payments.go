package payments

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

var (
    ErrNotFound       = errors.New("payment not found")
    ErrOrderNotFound  = errors.New("order not found")
    ErrInvalidPayment = errors.New("invalid payment")
)

type Payment struct {
    ID              string          `json:"id"`
    OrderID         string          `json:"order_id"`
    ClientPaymentID string          `json:"client_payment_id"`
    Mode            string          `json:"mode"`
    Direction       string          `json:"direction"`
    AmountMinor     int64           `json:"amount_minor"`
    Currency        string          `json:"currency"`
    Status          string          `json:"status"`
    Reference       *string         `json:"reference,omitempty"`
    Provider        *string         `json:"provider,omitempty"`
    ProviderPayload json.RawMessage `json:"provider_payload,omitempty"`
    RecordedBy      *string         `json:"recorded_by,omitempty"`
    CreatedAt       string          `json:"created_at"`
    UpdatedAt       string          `json:"updated_at"`
}

type CreateInput struct {
    ClientPaymentID string          `json:"client_payment_id"`
    Mode            string          `json:"mode"`
    Direction       string          `json:"direction,omitempty"`
    AmountMinor     int64           `json:"amount_minor"`
    Currency        string          `json:"currency,omitempty"`
    Status          string          `json:"status,omitempty"`
    Reference       *string         `json:"reference,omitempty"`
    Provider        *string         `json:"provider,omitempty"`
    ProviderPayload json.RawMessage `json:"provider_payload,omitempty"`
    RecordedBy      *string         `json:"recorded_by,omitempty"`
}

type Summary struct {
    OrderID      string `json:"order_id"`
    TotalMinor   int64  `json:"total_minor"`
    PaidMinor    int64  `json:"paid_minor"`
    BalanceMinor int64  `json:"balance_minor"`
    OrderStatus  string `json:"order_status"`
}

type RecordedHook func(context.Context, *sql.Tx, Payment, Summary) error

type Service struct {
    db           *database.DB
    recordedHook RecordedHook
}

func New(db *database.DB) *Service { return &Service{db: db} }
func (s *Service) SetRecordedHook(hook RecordedHook) { s.recordedHook = hook }

func (s *Service) Create(ctx context.Context, orderID string, input CreateInput) (Payment, Summary, error) {
    input.ClientPaymentID = strings.TrimSpace(input.ClientPaymentID)
    input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
    input.Direction = strings.ToLower(strings.TrimSpace(input.Direction))
    input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
    input.Status = strings.ToLower(strings.TrimSpace(input.Status))

    if input.ClientPaymentID == "" || input.AmountMinor <= 0 || !validMode(input.Mode) {
        return Payment{}, Summary{}, ErrInvalidPayment
    }
    if input.Direction == "" { input.Direction = "in" }
    if input.Direction != "in" && input.Direction != "out" { return Payment{}, Summary{}, ErrInvalidPayment }
    if input.Status == "" { input.Status = "captured" }
    if !validStatus(input.Status) { return Payment{}, Summary{}, ErrInvalidPayment }
    if len(input.ProviderPayload) > 0 && !json.Valid(input.ProviderPayload) { return Payment{}, Summary{}, ErrInvalidPayment }

    var created Payment
    var summary Summary
    err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
        var orderCurrency, orderStatus string
        var totalMinor int64
        if err := tx.QueryRowContext(ctx, `SELECT currency,total_minor,status FROM sales_orders WHERE id=?`, orderID).Scan(&orderCurrency,&totalMinor,&orderStatus); err != nil {
            if errors.Is(err, sql.ErrNoRows) { return ErrOrderNotFound }
            return err
        }
        if orderStatus == "cancelled" || orderStatus == "returned" { return ErrInvalidPayment }
        if input.Currency == "" { input.Currency = orderCurrency }
        if input.Currency != orderCurrency { return ErrInvalidPayment }

        existing, err := getByClientIDTx(ctx, tx, input.ClientPaymentID)
        if err == nil {
            if existing.OrderID != orderID || !paymentMatchesInput(existing, input) { return ErrInvalidPayment }
            created = existing
            summary, err = paymentSummaryTx(ctx, tx, orderID, totalMinor)
            return err
        }
        if !errors.Is(err, ErrNotFound) { return err }

        now := time.Now().UTC().Format(time.RFC3339Nano)
        id, err := newID("pay")
        if err != nil { return err }
        var providerPayload any
        if len(input.ProviderPayload) > 0 { providerPayload = string(input.ProviderPayload) }
        _, err = tx.ExecContext(ctx, `INSERT INTO payments(id,order_id,client_payment_id,mode,direction,amount_minor,currency,status,reference,provider,provider_payload_json,recorded_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
            id, orderID, input.ClientPaymentID, input.Mode, input.Direction, input.AmountMinor, input.Currency, input.Status, input.Reference, input.Provider, providerPayload, input.RecordedBy, now, now)
        if err != nil { return fmt.Errorf("insert payment: %w", err) }

        created = Payment{ID:id,OrderID:orderID,ClientPaymentID:input.ClientPaymentID,Mode:input.Mode,Direction:input.Direction,AmountMinor:input.AmountMinor,Currency:input.Currency,Status:input.Status,Reference:input.Reference,Provider:input.Provider,ProviderPayload:input.ProviderPayload,RecordedBy:input.RecordedBy,CreatedAt:now,UpdatedAt:now}
        snapshot, _ := json.Marshal(created)
        if _, err := tx.ExecContext(ctx, `INSERT INTO payment_snapshots(payment_id,version,snapshot_json,created_at) VALUES(?,?,?,?)`, id, 1, string(snapshot), now); err != nil { return err }
        summary, err = recalcOrderPaymentState(ctx, tx, orderID, totalMinor)
        if err != nil { return err }
        if s.recordedHook != nil { return s.recordedHook(ctx, tx, created, summary) }
        return nil
    })
    return created, summary, err
}

func (s *Service) ListForOrder(ctx context.Context, orderID string) ([]Payment, Summary, error) {
    var totalMinor int64
    if err := s.db.SQL().QueryRowContext(ctx, `SELECT total_minor FROM sales_orders WHERE id=?`, orderID).Scan(&totalMinor); err != nil {
        if errors.Is(err, sql.ErrNoRows) { return nil, Summary{}, ErrOrderNotFound }
        return nil, Summary{}, err
    }
    rows, err := s.db.SQL().QueryContext(ctx, `SELECT id,order_id,client_payment_id,mode,direction,amount_minor,currency,status,reference,provider,provider_payload_json,recorded_by,created_at,updated_at FROM payments WHERE order_id=? ORDER BY created_at,id`, orderID)
    if err != nil { return nil, Summary{}, err }
    defer rows.Close()
    items := []Payment{}
    for rows.Next() { p, err := scanPayment(rows); if err != nil { return nil, Summary{}, err }; items = append(items,p) }
    if err := rows.Err(); err != nil { return nil, Summary{}, err }
    summary, err := s.summary(ctx, orderID, totalMinor)
    return items, summary, err
}

func (s *Service) summary(ctx context.Context, orderID string, totalMinor int64) (Summary,error) {
    var paid int64
    if err := s.db.SQL().QueryRowContext(ctx, paymentTotalSQL, orderID).Scan(&paid); err != nil { return Summary{},err }
    var status string
    if err := s.db.SQL().QueryRowContext(ctx, `SELECT status FROM sales_orders WHERE id=?`, orderID).Scan(&status); err != nil { return Summary{},err }
    return buildSummary(orderID, totalMinor, paid, status),nil
}

const paymentTotalSQL = `SELECT COALESCE(SUM(CASE WHEN status='captured' AND direction='in' THEN amount_minor WHEN status IN ('captured','refunded') AND direction='out' THEN -amount_minor ELSE 0 END),0) FROM payments WHERE order_id=?`

func paymentSummaryTx(ctx context.Context, tx *sql.Tx, orderID string, totalMinor int64) (Summary,error) {
    var paid int64
    if err := tx.QueryRowContext(ctx, paymentTotalSQL, orderID).Scan(&paid); err != nil { return Summary{},err }
    var status string
    if err := tx.QueryRowContext(ctx, `SELECT status FROM sales_orders WHERE id=?`, orderID).Scan(&status); err != nil { return Summary{},err }
    return buildSummary(orderID, totalMinor, paid, status),nil
}

func buildSummary(orderID string, totalMinor, paid int64, status string) Summary {
    balance := totalMinor-paid; if balance < 0 { balance = 0 }
    return Summary{OrderID:orderID,TotalMinor:totalMinor,PaidMinor:paid,BalanceMinor:balance,OrderStatus:status}
}

func recalcOrderPaymentState(ctx context.Context, tx *sql.Tx, orderID string, totalMinor int64) (Summary,error) {
    var paid int64
    if err := tx.QueryRowContext(ctx, paymentTotalSQL, orderID).Scan(&paid); err != nil { return Summary{},err }
    status := "confirmed"
    if paid <= 0 { status = "confirmed" } else if paid < totalMinor { status = "partially_paid" } else { status = "paid" }
    now := time.Now().UTC().Format(time.RFC3339Nano)
    if _, err := tx.ExecContext(ctx, `UPDATE sales_orders SET status=?,version=version+1,updated_at=? WHERE id=? AND status NOT IN ('cancelled','returned')`, status, now, orderID); err != nil { return Summary{},err }
    return buildSummary(orderID, totalMinor, paid, status),nil
}

func getByClientIDTx(ctx context.Context, tx *sql.Tx, clientID string) (Payment,error) {
    p, err := scanPayment(tx.QueryRowContext(ctx, `SELECT id,order_id,client_payment_id,mode,direction,amount_minor,currency,status,reference,provider,provider_payload_json,recorded_by,created_at,updated_at FROM payments WHERE client_payment_id=?`, clientID))
    if errors.Is(err, sql.ErrNoRows) { return Payment{},ErrNotFound }
    return p,err
}

type scanner interface { Scan(dest ...any) error }
func scanPayment(s scanner) (Payment,error) {
    var p Payment; var payload sql.NullString
    if err := s.Scan(&p.ID,&p.OrderID,&p.ClientPaymentID,&p.Mode,&p.Direction,&p.AmountMinor,&p.Currency,&p.Status,&p.Reference,&p.Provider,&payload,&p.RecordedBy,&p.CreatedAt,&p.UpdatedAt); err != nil { return Payment{},err }
    if payload.Valid { p.ProviderPayload=json.RawMessage(payload.String) }
    return p,nil
}

func validMode(v string) bool { switch v { case "cash","bank","upi","card","credit","wallet": return true }; return false }
func validStatus(v string) bool { switch v { case "pending","captured","failed","voided","refunded": return true }; return false }
func newID(prefix string) (string,error) { var b [16]byte; if _,err:=rand.Read(b[:]); err!=nil { return "",err }; return prefix+"_"+hex.EncodeToString(b[:]),nil }
