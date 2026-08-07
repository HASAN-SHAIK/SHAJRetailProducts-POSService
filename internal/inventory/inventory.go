package inventory

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

type Service struct{ db *database.DB }

func New(db *database.DB) *Service { return &Service{db: db} }

type Balance struct {
    StoreID       string `json:"store_id"`
    ProductID     string `json:"product_id"`
    OnHandMilli   int64  `json:"on_hand_milli"`
    ReservedMilli int64  `json:"reserved_milli"`
    AvailableMilli int64 `json:"available_milli"`
    Version       int    `json:"version"`
    UpdatedAt     string `json:"updated_at"`
}

type Movement struct {
    ID                 string  `json:"id"`
    StoreID            string  `json:"store_id"`
    ProductID          string  `json:"product_id"`
    MovementType       string  `json:"movement_type"`
    QuantityDeltaMilli int64   `json:"quantity_delta_milli"`
    ReferenceType      *string `json:"reference_type,omitempty"`
    ReferenceID        *string `json:"reference_id,omitempty"`
    OrderItemID        *string `json:"order_item_id,omitempty"`
    BalanceAfterMilli  int64   `json:"balance_after_milli"`
    OccurredAt         string  `json:"occurred_at"`
}

func (s *Service) ApplySaleTx(ctx context.Context, tx *sql.Tx, order orders.Order) error {
    now := time.Now().UTC().Format(time.RFC3339Nano)
    for _, item := range order.Items {
        var track int
        if err := tx.QueryRowContext(ctx, `SELECT track_inventory FROM catalog_products WHERE id=?`, item.ProductID).Scan(&track); err != nil {
            return fmt.Errorf("load inventory policy: %w", err)
        }
        if track == 0 { continue }

        var already int
        if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id=? AND movement_type='sale_issue'`, item.ID).Scan(&already); err != nil {
            return err
        }
        if already > 0 { continue }

        if _, err := tx.ExecContext(ctx, `
            INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at)
            VALUES(?,?,0,0,1,?)
            ON CONFLICT(store_id,product_id) DO NOTHING`, order.StoreID, item.ProductID, now); err != nil { return err }

        if _, err := tx.ExecContext(ctx, `
            UPDATE inventory_balances
            SET on_hand_milli=on_hand_milli-?, version=version+1, updated_at=?
            WHERE store_id=? AND product_id=?`, item.QuantityMilli, now, order.StoreID, item.ProductID); err != nil { return err }

        var after int64
        if err := tx.QueryRowContext(ctx, `SELECT on_hand_milli FROM inventory_balances WHERE store_id=? AND product_id=?`, order.StoreID, item.ProductID).Scan(&after); err != nil { return err }
        refType, refID, orderItemID := "sale_order", order.ID, item.ID
        movementID := fmt.Sprintf("mov_%d_%d", time.Now().UTC().UnixNano(), item.LineNo)
        if _, err := tx.ExecContext(ctx, `
            INSERT INTO inventory_movements(id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at)
            VALUES(?,?,?,'sale_issue',?,?,?,?,?,?,?)`,
            movementID, order.StoreID, item.ProductID, -item.QuantityMilli, refType, refID, orderItemID, after, now, now); err != nil { return err }
    }
    return nil
}

func (s *Service) GetBalance(ctx context.Context, storeID, productID string) (Balance, error) {
    var b Balance
    err := s.db.SQL().QueryRowContext(ctx, `
        SELECT store_id,product_id,on_hand_milli,reserved_milli,version,updated_at
        FROM inventory_balances WHERE store_id=? AND product_id=?`, storeID, productID).
        Scan(&b.StoreID,&b.ProductID,&b.OnHandMilli,&b.ReservedMilli,&b.Version,&b.UpdatedAt)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            b = Balance{StoreID: storeID, ProductID: productID}
            return b, nil
        }
        return Balance{}, err
    }
    b.AvailableMilli = b.OnHandMilli - b.ReservedMilli
    return b, nil
}

func (s *Service) ListMovements(ctx context.Context, storeID, productID string, limit int) ([]Movement, error) {
    if limit <= 0 || limit > 200 { limit = 50 }
    q := `SELECT id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,order_item_id,balance_after_milli,occurred_at FROM inventory_movements WHERE store_id=?`
    args := []any{storeID}
    if productID != "" { q += ` AND product_id=?`; args = append(args, productID) }
    q += ` ORDER BY occurred_at DESC LIMIT ?`; args = append(args, limit)
    rows, err := s.db.SQL().QueryContext(ctx, q, args...)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []Movement
    for rows.Next() {
        var m Movement
        if err := rows.Scan(&m.ID,&m.StoreID,&m.ProductID,&m.MovementType,&m.QuantityDeltaMilli,&m.ReferenceType,&m.ReferenceID,&m.OrderItemID,&m.BalanceAfterMilli,&m.OccurredAt); err != nil { return nil, err }
        out = append(out, m)
    }
    return out, rows.Err()
}
