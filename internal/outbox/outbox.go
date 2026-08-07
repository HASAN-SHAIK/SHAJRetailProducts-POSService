package outbox

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

type Service struct{ db *database.DB }

func New(db *database.DB) *Service { return &Service{db: db} }

type Event struct {
    ID               string          `json:"id"`
    AggregateType    string          `json:"aggregate_type"`
    AggregateID      string          `json:"aggregate_id"`
    AggregateVersion int             `json:"aggregate_version"`
    EventType        string          `json:"event_type"`
    SchemaVersion    int             `json:"schema_version"`
    OrderingKey      string          `json:"ordering_key"`
    Payload          json.RawMessage `json:"payload"`
    Metadata         json.RawMessage `json:"metadata"`
    Status           string          `json:"status"`
    AttemptCount     int             `json:"attempt_count"`
    AvailableAt      string          `json:"available_at"`
    CreatedAt        string          `json:"created_at"`
    PublishedAt      *string         `json:"published_at,omitempty"`
}

type Status struct {
    Pending    int64   `json:"pending"`
    Processing int64   `json:"processing"`
    Published  int64   `json:"published"`
    Failed     int64   `json:"failed"`
    DeadLetter int64   `json:"dead_letter"`
    OldestPendingAt *string `json:"oldest_pending_at,omitempty"`
}

type SaleCompletedPayload struct {
    Order           orders.Order    `json:"order"`
    Receipt         json.RawMessage `json:"receipt"`
    Inventory       json.RawMessage `json:"inventory_movements"`
    Payments        json.RawMessage `json:"payments"`
}

// ApplySaleCompletedTx appends the durable integration event in the same
// SQLite transaction that completes the sale, issues inventory, and creates
// the immutable receipt. If this insert fails, the entire sale completion
// transaction rolls back.
func (s *Service) ApplySaleCompletedTx(ctx context.Context, tx *sql.Tx, order orders.Order) error {
    receipt, err := loadReceiptTx(ctx, tx, order.ID)
    if err != nil { return err }
    inventory, err := loadInventoryTx(ctx, tx, order.ID)
    if err != nil { return err }
    payments, err := loadPaymentsTx(ctx, tx, order.ID)
    if err != nil { return err }

    payload, err := json.Marshal(SaleCompletedPayload{
        Order: order, Receipt: receipt, Inventory: inventory, Payments: payments,
    })
    if err != nil { return fmt.Errorf("marshal sale completed event: %w", err) }

    metadata, err := json.Marshal(map[string]any{
        "source": "pos_service",
        "store_id": order.StoreID,
        "terminal_id": order.TerminalID,
        "occurred_at": order.CompletedAt,
    })
    if err != nil { return err }

    now := time.Now().UTC().Format(time.RFC3339Nano)
    eventID := fmt.Sprintf("evt_%d", time.Now().UTC().UnixNano())
    orderingKey := "sales_order:" + order.ID
    _, err = tx.ExecContext(ctx, `
        INSERT INTO outbox_events(
            id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
            ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
        ) VALUES(?,?,?,?,'sale.completed',1,?,?,?,'pending',0,?,?)
        ON CONFLICT(aggregate_type,aggregate_id,aggregate_version,event_type) DO NOTHING`,
        eventID, "sales_order", order.ID, order.Version, orderingKey, string(payload), string(metadata), now, now,
    )
    return err
}

func (s *Service) GetStatus(ctx context.Context) (Status, error) {
    var st Status
    rows, err := s.db.SQL().QueryContext(ctx, `SELECT status,COUNT(*) FROM outbox_events GROUP BY status`)
    if err != nil { return Status{}, err }
    defer rows.Close()
    for rows.Next() {
        var status string
        var count int64
        if err := rows.Scan(&status,&count); err != nil { return Status{}, err }
        switch status {
        case "pending": st.Pending = count
        case "processing": st.Processing = count
        case "published": st.Published = count
        case "failed": st.Failed = count
        case "dead_letter": st.DeadLetter = count
        }
    }
    if err := rows.Err(); err != nil { return Status{}, err }
    var oldest sql.NullString
    if err := s.db.SQL().QueryRowContext(ctx, `SELECT MIN(created_at) FROM outbox_events WHERE status IN ('pending','failed')`).Scan(&oldest); err != nil {
        return Status{}, err
    }
    if oldest.Valid { st.OldestPendingAt = &oldest.String }
    return st, nil
}

func (s *Service) ListPending(ctx context.Context, limit int) ([]Event, error) {
    if limit <= 0 || limit > 200 { limit = 50 }
    rows, err := s.db.SQL().QueryContext(ctx, `
        SELECT id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
               payload_json,metadata_json,status,attempt_count,available_at,created_at,published_at
        FROM outbox_events
        WHERE status IN ('pending','failed') AND available_at <= ?
        ORDER BY created_at,id LIMIT ?`, time.Now().UTC().Format(time.RFC3339Nano), limit)
    if err != nil { return nil, err }
    defer rows.Close()
    var events []Event
    for rows.Next() {
        var e Event
        var payload, metadata string
        if err := rows.Scan(&e.ID,&e.AggregateType,&e.AggregateID,&e.AggregateVersion,&e.EventType,&e.SchemaVersion,&e.OrderingKey,&payload,&metadata,&e.Status,&e.AttemptCount,&e.AvailableAt,&e.CreatedAt,&e.PublishedAt); err != nil {
            return nil, err
        }
        e.Payload = json.RawMessage(payload)
        e.Metadata = json.RawMessage(metadata)
        events = append(events, e)
    }
    return events, rows.Err()
}

func loadReceiptTx(ctx context.Context, tx *sql.Tx, orderID string) (json.RawMessage, error) {
    var id, receiptNumber, documentType, storeID, currency, snapshotJSON, snapshotSHA, issuedAt string
    var terminalID, customerID sql.NullString
    var total, paid, balance int64
    err := tx.QueryRowContext(ctx, `
        SELECT id,receipt_number,document_type,store_id,terminal_id,customer_id,currency,total_minor,
               paid_minor,balance_minor,snapshot_json,snapshot_sha256,issued_at
        FROM receipts WHERE order_id=?`, orderID).
        Scan(&id,&receiptNumber,&documentType,&storeID,&terminalID,&customerID,&currency,&total,&paid,&balance,&snapshotJSON,&snapshotSHA,&issuedAt)
    if errors.Is(err, sql.ErrNoRows) { return nil, errors.New("receipt_missing_for_completed_sale") }
    if err != nil { return nil, err }
    raw, err := json.Marshal(map[string]any{
        "id": id, "receipt_number": receiptNumber, "document_type": documentType, "store_id": storeID,
        "terminal_id": nullableString(terminalID), "customer_id": nullableString(customerID), "currency": currency,
        "total_minor": total, "paid_minor": paid, "balance_minor": balance,
        "snapshot": json.RawMessage(snapshotJSON), "snapshot_sha256": snapshotSHA, "issued_at": issuedAt,
    })
    return raw, err
}

func loadInventoryTx(ctx context.Context, tx *sql.Tx, orderID string) (json.RawMessage, error) {
    rows, err := tx.QueryContext(ctx, `
        SELECT id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,
               order_item_id,balance_after_milli,occurred_at
        FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=? ORDER BY occurred_at,id`, orderID)
    if err != nil { return nil, err }
    defer rows.Close()
    var items []map[string]any
    for rows.Next() {
        var id, storeID, productID, movementType, occurredAt string
        var refType, refID, orderItemID sql.NullString
        var quantity, balance int64
        if err := rows.Scan(&id,&storeID,&productID,&movementType,&quantity,&refType,&refID,&orderItemID,&balance,&occurredAt); err != nil { return nil, err }
        items = append(items, map[string]any{"id":id,"store_id":storeID,"product_id":productID,"movement_type":movementType,"quantity_delta_milli":quantity,"reference_type":nullableString(refType),"reference_id":nullableString(refID),"order_item_id":nullableString(orderItemID),"balance_after_milli":balance,"occurred_at":occurredAt})
    }
    if err := rows.Err(); err != nil { return nil, err }
    raw, err := json.Marshal(items)
    return raw, err
}

func loadPaymentsTx(ctx context.Context, tx *sql.Tx, orderID string) (json.RawMessage, error) {
    rows, err := tx.QueryContext(ctx, `
        SELECT id,client_payment_id,mode,direction,amount_minor,currency,status,reference,provider,created_at
        FROM payments WHERE order_id=? ORDER BY created_at,id`, orderID)
    if err != nil { return nil, err }
    defer rows.Close()
    var items []map[string]any
    for rows.Next() {
        var id, clientID, mode, direction, currency, status, createdAt string
        var reference, provider sql.NullString
        var amount int64
        if err := rows.Scan(&id,&clientID,&mode,&direction,&amount,&currency,&status,&reference,&provider,&createdAt); err != nil { return nil, err }
        items = append(items, map[string]any{"id":id,"client_payment_id":clientID,"mode":mode,"direction":direction,"amount_minor":amount,"currency":currency,"status":status,"reference":nullableString(reference),"provider":nullableString(provider),"created_at":createdAt})
    }
    if err := rows.Err(); err != nil { return nil, err }
    raw, err := json.Marshal(items)
    return raw, err
}

func nullableString(v sql.NullString) any {
    if !v.Valid { return nil }
    return v.String
}
