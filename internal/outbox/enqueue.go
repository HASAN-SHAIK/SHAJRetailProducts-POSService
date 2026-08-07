package outbox

import (
    "context"
    "database/sql"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

// EnqueueSaleCompletedTx appends the canonical sale.completed integration
// event using the caller's existing transaction. It intentionally does not
// start or commit a transaction of its own.
func EnqueueSaleCompletedTx(ctx context.Context, tx *sql.Tx, orderID string, aggregateVersion int) error {
    order, err := loadOrderTx(ctx, tx, orderID)
    if err != nil {
        return err
    }
    order.Version = aggregateVersion
    svc := &Service{}
    return svc.ApplySaleCompletedTx(ctx, tx, order)
}

func loadOrderTx(ctx context.Context, tx *sql.Tx, orderID string) (order orders.Order, err error) {
    err = tx.QueryRowContext(ctx, `
        SELECT id,client_order_id,store_id,terminal_id,customer_id,status,currency,subtotal_minor,
               discount_minor,tax_minor,total_minor,notes,version,completed_at,created_at,updated_at
        FROM sales_orders WHERE id=?`, orderID).
        Scan(&order.ID,&order.ClientOrderID,&order.StoreID,&order.TerminalID,&order.CustomerID,&order.Status,&order.Currency,
            &order.SubtotalMinor,&order.DiscountMinor,&order.TaxMinor,&order.TotalMinor,&order.Notes,&order.Version,
            &order.CompletedAt,&order.CreatedAt,&order.UpdatedAt)
    if err != nil { return order, err }

    rows, err := tx.QueryContext(ctx, `
        SELECT id,line_no,product_id,sku,product_name,barcode,quantity_milli,unit_price_minor,
               discount_minor,tax_minor,line_total_minor,tax_code
        FROM sales_order_items WHERE order_id=? ORDER BY line_no`, orderID)
    if err != nil { return order, err }
    defer rows.Close()
    for rows.Next() {
        var item orders.Item
        if err := rows.Scan(&item.ID,&item.LineNo,&item.ProductID,&item.SKU,&item.ProductName,&item.Barcode,&item.QuantityMilli,
            &item.UnitPriceMinor,&item.DiscountMinor,&item.TaxMinor,&item.LineTotalMinor,&item.TaxCode); err != nil {
            return order, err
        }
        order.Items = append(order.Items, item)
    }
    return order, rows.Err()
}
