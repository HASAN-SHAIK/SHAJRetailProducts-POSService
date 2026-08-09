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

type SalePartialReturnedLine struct {
	OrderItemID   string `json:"order_item_id"`
	QuantityMilli int64  `json:"quantity_milli"`
	RefundMinor   int64  `json:"refund_minor"`
}

type SalePartialReturnedPayload struct {
	ReturnID         string                    `json:"return_id"`
	Order            orders.Order              `json:"order"`
	RefundMinor      int64                     `json:"refund_minor"`
	Lines            []SalePartialReturnedLine `json:"lines"`
	ApprovedByUserID string                    `json:"approved_by_user_id"`
	ApprovalReason   string                    `json:"approval_reason"`
	ReturnedAt       string                    `json:"returned_at"`
}

// ApplySalePartialReturnedTx appends one item-level return operation in the same
// transaction as its ledger, tender reversal, stock restoration, and order audit.
// A final operation still emits this item-level fact before sale.returned so Central
// receives the last quantities/refund allocation as well as the terminal lifecycle.
func (s *Service) ApplySalePartialReturnedTx(ctx context.Context, tx *sql.Tx, order orders.Order, returnID string, refundMinor int64, lines []SalePartialReturnedLine, approvedByUserID, reason string) error {
	eventOrder := order
	// POS payment state may become paid/partially_paid as tender reversals are
	// applied. Central models the sale lifecycle separately: an item-level return
	// remains a completed sale until the final remaining quantity is consumed.
	// Canonicalize only the durable event vocabulary; keep the local DB state intact.
	if eventOrder.Status != "returned" {
		eventOrder.Status = "completed"
	}
	payload, err := json.Marshal(SalePartialReturnedPayload{
		ReturnID: returnID,
		Order: eventOrder,
		RefundMinor: refundMinor,
		Lines: lines,
		ApprovedByUserID: approvedByUserID,
		ApprovalReason: reason,
		ReturnedAt: eventOrder.UpdatedAt,
	})
	if err != nil { return err }
	metadata, err := json.Marshal(map[string]any{
		"source": "pos_service", "store_id": order.StoreID, "terminal_id": order.TerminalID,
		"occurred_at": order.UpdatedAt,
	})
	if err != nil { return err }
	now := time.Now().UTC().Format(time.RFC3339Nano)
	eventID := fmt.Sprintf("evt_sale_partial_returned_%s", returnID)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
			ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
		) VALUES(?,?,?,?,'sale.partial_returned',1,?,?,?,'pending',0,?,?)
		ON CONFLICT(aggregate_type,aggregate_id,aggregate_version,event_type) DO NOTHING`,
		eventID, "sales_order", order.ID, order.Version, "sales_order:"+order.ID,
		string(payload), string(metadata), now, now)
	return err
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
