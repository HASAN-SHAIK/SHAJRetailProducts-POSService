package receipts

import (
    "context"
    "crypto/sha256"
    "database/sql"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

var ErrNotFound = errors.New("receipt not found")

type Service struct{ db *database.DB }

func New(db *database.DB) *Service { return &Service{db: db} }

type PaymentLine struct {
    ID          string  `json:"id"`
    Mode        string  `json:"mode"`
    Direction   string  `json:"direction"`
    AmountMinor int64   `json:"amount_minor"`
    Currency    string  `json:"currency"`
    Status      string  `json:"status"`
    Reference   *string `json:"reference,omitempty"`
    Provider    *string `json:"provider,omitempty"`
    CreatedAt   string  `json:"created_at"`
}

type CustomerSnapshot struct {
    ID           string  `json:"id"`
    CustomerCode *string `json:"customer_code,omitempty"`
    Name         string  `json:"name"`
    Phone        *string `json:"phone,omitempty"`
    Email        *string `json:"email,omitempty"`
    TaxID        *string `json:"tax_id,omitempty"`
}

type Snapshot struct {
    Order    orders.Order      `json:"order"`
    Customer *CustomerSnapshot `json:"customer,omitempty"`
    Payments []PaymentLine     `json:"payments"`
}

type Receipt struct {
    ID             string   `json:"id"`
    OrderID        string   `json:"order_id"`
    ReceiptNumber  string   `json:"receipt_number"`
    DocumentType   string   `json:"document_type"`
    StoreID        string   `json:"store_id"`
    TerminalID     *string  `json:"terminal_id,omitempty"`
    CustomerID     *string  `json:"customer_id,omitempty"`
    Currency       string   `json:"currency"`
    TotalMinor     int64    `json:"total_minor"`
    PaidMinor      int64    `json:"paid_minor"`
    BalanceMinor   int64    `json:"balance_minor"`
    Snapshot       Snapshot `json:"snapshot"`
    SnapshotSHA256 string   `json:"snapshot_sha256"`
    IssuedAt       string   `json:"issued_at"`
}

// ApplyCompletionTx creates exactly one immutable receipt inside the same
// transaction as inventory issuance and order completion.
func (s *Service) ApplyCompletionTx(ctx context.Context, tx *sql.Tx, order orders.Order) error {
    var existing string
    err := tx.QueryRowContext(ctx, `SELECT id FROM receipts WHERE order_id=?`, order.ID).Scan(&existing)
    if err == nil {
        return nil
    }
    if !errors.Is(err, sql.ErrNoRows) {
        return err
    }

    payments, paid, err := loadPaymentsTx(ctx, tx, order.ID, order.Currency)
    if err != nil {
        return err
    }
    customer, err := loadCustomerTx(ctx, tx, order.CustomerID)
    if err != nil {
        return err
    }

    snapshot := Snapshot{Order: order, Customer: customer, Payments: payments}
    raw, err := json.Marshal(snapshot)
    if err != nil {
        return fmt.Errorf("marshal receipt snapshot: %w", err)
    }
    digest := sha256.Sum256(raw)
    now := time.Now().UTC().Format(time.RFC3339Nano)
    receiptNumber, err := nextReceiptNumberTx(ctx, tx, order.StoreID, order.TerminalID, now)
    if err != nil {
        return err
    }
    balance := order.TotalMinor - paid
    if balance < 0 {
        balance = 0
    }

    _, err = tx.ExecContext(ctx, `
        INSERT INTO receipts(
            id,order_id,receipt_number,document_type,store_id,terminal_id,customer_id,
            currency,total_minor,paid_minor,balance_minor,snapshot_json,snapshot_sha256,issued_at,created_at
        ) VALUES(?,?,?,'receipt',?,?,?,?,?,?,?,?,?,?,?,?)`,
        newID("rcp"), order.ID, receiptNumber, order.StoreID, order.TerminalID, order.CustomerID,
        order.Currency, order.TotalMinor, paid, balance, string(raw), hex.EncodeToString(digest[:]), now, now,
    )
    return err
}

func (s *Service) GetByOrder(ctx context.Context, orderID string) (Receipt, error) {
    return s.getOne(ctx, `WHERE order_id=?`, orderID)
}

func (s *Service) Get(ctx context.Context, id string) (Receipt, error) {
    return s.getOne(ctx, `WHERE id=?`, id)
}

func (s *Service) getOne(ctx context.Context, where string, value any) (Receipt, error) {
    var r Receipt
    var raw string
    err := s.db.SQL().QueryRowContext(ctx, `
        SELECT id,order_id,receipt_number,document_type,store_id,terminal_id,customer_id,
               currency,total_minor,paid_minor,balance_minor,snapshot_json,snapshot_sha256,issued_at
        FROM receipts `+where, value).Scan(
        &r.ID,&r.OrderID,&r.ReceiptNumber,&r.DocumentType,&r.StoreID,&r.TerminalID,&r.CustomerID,
        &r.Currency,&r.TotalMinor,&r.PaidMinor,&r.BalanceMinor,&raw,&r.SnapshotSHA256,&r.IssuedAt,
    )
    if errors.Is(err, sql.ErrNoRows) {
        return Receipt{}, ErrNotFound
    }
    if err != nil {
        return Receipt{}, err
    }
    if err := json.Unmarshal([]byte(raw), &r.Snapshot); err != nil {
        return Receipt{}, fmt.Errorf("decode receipt snapshot: %w", err)
    }
    return r, nil
}

func loadPaymentsTx(ctx context.Context, tx *sql.Tx, orderID, currency string) ([]PaymentLine, int64, error) {
    rows, err := tx.QueryContext(ctx, `
        SELECT id,mode,direction,amount_minor,currency,status,reference,provider,created_at
        FROM payments WHERE order_id=? ORDER BY created_at,id`, orderID)
    if err != nil {
        return nil, 0, err
    }
    defer rows.Close()
    var out []PaymentLine
    var paid int64
    for rows.Next() {
        var p PaymentLine
        if err := rows.Scan(&p.ID,&p.Mode,&p.Direction,&p.AmountMinor,&p.Currency,&p.Status,&p.Reference,&p.Provider,&p.CreatedAt); err != nil {
            return nil, 0, err
        }
        out = append(out, p)
        if p.Currency == currency && p.Status == "captured" {
            if p.Direction == "in" {
                paid += p.AmountMinor
            } else if p.Direction == "out" {
                paid -= p.AmountMinor
            }
        }
    }
    return out, paid, rows.Err()
}

func loadCustomerTx(ctx context.Context, tx *sql.Tx, customerID *string) (*CustomerSnapshot, error) {
    if customerID == nil || strings.TrimSpace(*customerID) == "" {
        return nil, nil
    }
    var c CustomerSnapshot
    err := tx.QueryRowContext(ctx, `
        SELECT id,customer_code,name,phone,email,tax_id FROM customers WHERE id=?`, *customerID).
        Scan(&c.ID,&c.CustomerCode,&c.Name,&c.Phone,&c.Email,&c.TaxID)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    return &c, nil
}

func nextReceiptNumberTx(ctx context.Context, tx *sql.Tx, storeID string, terminalID *string, issuedAt string) (string, error) {
    t, err := time.Parse(time.RFC3339Nano, issuedAt)
    if err != nil {
        return "", err
    }
    terminal := "LOCAL"
    if terminalID != nil && strings.TrimSpace(*terminalID) != "" {
        terminal = sanitize(*terminalID)
    }
    store := sanitize(storeID)
    date := t.UTC().Format("20060102")
    scope := "receipt:" + store + ":" + terminal + ":" + date
    now := t.UTC().Format(time.RFC3339Nano)

    if _, err := tx.ExecContext(ctx, `
        INSERT INTO local_sequences(scope,next_value,updated_at) VALUES(?,2,?)
        ON CONFLICT(scope) DO UPDATE SET next_value=local_sequences.next_value+1, updated_at=excluded.updated_at`,
        scope, now); err != nil {
        return "", err
    }
    var next int64
    if err := tx.QueryRowContext(ctx, `SELECT next_value FROM local_sequences WHERE scope=?`, scope).Scan(&next); err != nil {
        return "", err
    }
    sequence := next - 1
    return fmt.Sprintf("%s-%s-%s-%06d", store, terminal, date, sequence), nil
}

func sanitize(v string) string {
    v = strings.ToUpper(strings.TrimSpace(v))
    var b strings.Builder
    for _, r := range v {
        if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
            b.WriteRune(r)
        }
    }
    if b.Len() == 0 {
        return "LOCAL"
    }
    if b.Len() > 12 {
        return b.String()[:12]
    }
    return b.String()
}

func newID(prefix string) string {
    return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
