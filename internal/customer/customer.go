package customer

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/hex"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

var ErrNotFound = errors.New("customer not found")

type Customer struct {
    ID               string  `json:"id"`
    CustomerCode     *string `json:"customer_code,omitempty"`
    Name             string  `json:"name"`
    Phone            *string `json:"phone,omitempty"`
    Email            *string `json:"email,omitempty"`
    TaxID            *string `json:"tax_id,omitempty"`
    CreditLimitMinor int64   `json:"credit_limit_minor"`
    OutstandingMinor int64   `json:"outstanding_minor"`
    Currency         string  `json:"currency"`
    Status           string  `json:"status"`
    LocalVersion     int64   `json:"local_version"`
    SyncState        string  `json:"sync_state"`
    UpdatedAt        string  `json:"updated_at"`
}

type UpsertInput struct {
    CustomerCode     *string `json:"customer_code,omitempty"`
    Name             string  `json:"name"`
    Phone            *string `json:"phone,omitempty"`
    Email            *string `json:"email,omitempty"`
    TaxID            *string `json:"tax_id,omitempty"`
    CreditLimitMinor int64   `json:"credit_limit_minor"`
    Currency         string  `json:"currency,omitempty"`
}

type Repository struct { db *database.DB }

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Get(ctx context.Context, id string) (Customer, error) {
    row := r.db.SQL().QueryRowContext(ctx, `
        SELECT id, customer_code, name, phone, email, tax_id, credit_limit_minor,
               outstanding_minor, currency, status, local_version, sync_state, updated_at
        FROM customers WHERE id = ?`, strings.TrimSpace(id))
    c, err := scanCustomer(row)
    if errors.Is(err, sql.ErrNoRows) { return Customer{}, ErrNotFound }
    if err != nil { return Customer{}, fmt.Errorf("get customer: %w", err) }
    return c, nil
}

func (r *Repository) Search(ctx context.Context, query string, limit int) ([]Customer, error) {
    query = strings.TrimSpace(query)
    if limit <= 0 || limit > 200 { limit = 50 }
    like := "%" + query + "%"
    rows, err := r.db.SQL().QueryContext(ctx, `
        SELECT id, customer_code, name, phone, email, tax_id, credit_limit_minor,
               outstanding_minor, currency, status, local_version, sync_state, updated_at
        FROM customers
        WHERE status <> 'inactive'
          AND (? = '' OR name LIKE ? COLLATE NOCASE OR phone LIKE ? OR email LIKE ? COLLATE NOCASE OR customer_code LIKE ? COLLATE NOCASE)
        ORDER BY name
        LIMIT ?`, query, like, like, like, like, limit)
    if err != nil { return nil, fmt.Errorf("search customers: %w", err) }
    defer rows.Close()
    out := make([]Customer, 0)
    for rows.Next() {
        c, err := scanCustomer(rows)
        if err != nil { return nil, err }
        out = append(out, c)
    }
    return out, rows.Err()
}

func (r *Repository) Create(ctx context.Context, input UpsertInput) (Customer, error) {
    if err := validateInput(&input); err != nil { return Customer{}, err }
    id, err := newID()
    if err != nil { return Customer{}, fmt.Errorf("generate customer id: %w", err) }
    now := time.Now().UTC().Format(time.RFC3339Nano)
    _, err = r.db.SQL().ExecContext(ctx, `
        INSERT INTO customers(
            id, customer_code, name, phone, email, tax_id, credit_limit_minor,
            outstanding_minor, currency, status, source_updated_at, created_at, updated_at,
            local_version, sync_state
        ) VALUES(?,?,?,?,?,?,?,0,?,'active',NULL,?,?,1,'pending')`,
        id, cleanPtr(input.CustomerCode), strings.TrimSpace(input.Name), cleanPtr(input.Phone), cleanPtr(input.Email), cleanPtr(input.TaxID),
        input.CreditLimitMinor, input.Currency, now, now)
    if err != nil { return Customer{}, fmt.Errorf("create customer: %w", err) }
    return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, input UpsertInput) (Customer, error) {
    if err := validateInput(&input); err != nil { return Customer{}, err }
    id = strings.TrimSpace(id)
    now := time.Now().UTC().Format(time.RFC3339Nano)
    result, err := r.db.SQL().ExecContext(ctx, `
        UPDATE customers SET
            customer_code = ?, name = ?, phone = ?, email = ?, tax_id = ?,
            credit_limit_minor = ?, currency = ?, local_version = local_version + 1,
            sync_state = 'pending', updated_at = ?
        WHERE id = ?`, cleanPtr(input.CustomerCode), strings.TrimSpace(input.Name), cleanPtr(input.Phone), cleanPtr(input.Email),
        cleanPtr(input.TaxID), input.CreditLimitMinor, input.Currency, now, id)
    if err != nil { return Customer{}, fmt.Errorf("update customer: %w", err) }
    affected, err := result.RowsAffected()
    if err != nil { return Customer{}, err }
    if affected == 0 { return Customer{}, ErrNotFound }
    return r.Get(ctx, id)
}

func validateInput(input *UpsertInput) error {
    input.Name = strings.TrimSpace(input.Name)
    if input.Name == "" { return errors.New("customer_name_required") }
    if input.CreditLimitMinor < 0 { return errors.New("invalid_credit_limit") }
    input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
    if input.Currency == "" { input.Currency = "INR" }
    if len(input.Currency) != 3 { return errors.New("invalid_currency") }
    return nil
}

func cleanPtr(value *string) any {
    if value == nil { return nil }
    trimmed := strings.TrimSpace(*value)
    if trimmed == "" { return nil }
    return trimmed
}

func newID() (string, error) {
    raw := make([]byte, 16)
    if _, err := rand.Read(raw); err != nil { return "", err }
    return "cus_" + hex.EncodeToString(raw), nil
}

type scanner interface { Scan(dest ...any) error }

func scanCustomer(s scanner) (Customer, error) {
    var c Customer
    err := s.Scan(&c.ID, &c.CustomerCode, &c.Name, &c.Phone, &c.Email, &c.TaxID, &c.CreditLimitMinor,
        &c.OutstandingMinor, &c.Currency, &c.Status, &c.LocalVersion, &c.SyncState, &c.UpdatedAt)
    return c, err
}
