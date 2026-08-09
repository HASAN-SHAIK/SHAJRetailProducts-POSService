package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

type SaleReturnedPayload struct {
	Order            orders.Order    `json:"order"`
	Inventory        json.RawMessage `json:"inventory_movements"`
	Payments         json.RawMessage `json:"payments"`
	ApprovedByUserID string          `json:"approved_by_user_id"`
	ApprovalReason   string          `json:"approval_reason"`
}

// ApplySaleReturnedTx appends the final refund fact in the same transaction as
// payment reversal, stock restoration, and the returned-order state change.
func (s *Service) ApplySaleReturnedTx(ctx context.Context, tx *sql.Tx, order orders.Order, approvedByUserID, reason string) error {
	inventory, err := loadInventoryTx(ctx, tx, order.ID)
	if err != nil { return err }
	payments, err := loadPaymentsTx(ctx, tx, order.ID)
	if err != nil { return err }
	payload, err := json.Marshal(SaleReturnedPayload{
		Order: order,
		Inventory: inventory,
		Payments: payments,
		ApprovedByUserID: approvedByUserID,
		ApprovalReason: reason,
	})
	if err != nil { return err }
	metadata, err := json.Marshal(map[string]any{
		"source": "pos_service", "store_id": order.StoreID, "terminal_id": order.TerminalID,
		"occurred_at": order.UpdatedAt,
	})
	if err != nil { return err }
	now := time.Now().UTC().Format(time.RFC3339Nano)
	eventID := fmt.Sprintf("evt_sale_returned_%s_v%d", order.ID, order.Version)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
			ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
		) VALUES(?,?,?,?,'sale.returned',1,?,?,?,'pending',0,?,?)
		ON CONFLICT(aggregate_type,aggregate_id,aggregate_version,event_type) DO NOTHING`,
		eventID, "sales_order", order.ID, order.Version, "sales_order:"+order.ID,
		string(payload), string(metadata), now, now)
	return err
}
