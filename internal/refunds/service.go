package refunds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
)

var ErrExistingReversal = errors.New("existing payment reversal requires manual reconciliation")

type Service struct {
	db        *database.DB
	orders    *orders.Service
	payments  *payments.Service
	inventory *inventory.Service
	outbox    *outbox.Service
}

func New(db *database.DB, orderService *orders.Service, paymentService *payments.Service, inventoryService *inventory.Service) *Service {
	return &Service{db: db, orders: orderService, payments: paymentService, inventory: inventoryService, outbox: outbox.New(db)}
}

// RefundFullSale preserves the legacy single-actor contract.
func (s *Service) RefundFullSale(ctx context.Context, orderID, approvedByUserID, reason string) (orders.Order, error) {
	return s.RefundFullSaleWithActors(ctx, orderID, approvedByUserID, approvedByUserID, reason)
}

// RefundFullSaleWithActors compensates every captured tender, restores
// sale-issued stock, marks the completed order returned, and emits a durable
// sale.returned fact while preserving refund initiator and manager approver as
// distinct identities inside one SQLite transaction.
func (s *Service) RefundFullSaleWithActors(ctx context.Context, orderID, refundedByUserID, approvedByUserID, reason string) (orders.Order, error) {
	return s.orders.RefundWithActors(ctx, orderID, refundedByUserID, approvedByUserID, reason,
		func(ctx context.Context, tx *sql.Tx, order orders.Order, paidMinor int64) error {
			var reversalCount int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id=? AND direction='out' AND status IN ('captured','refunded')`, order.ID).Scan(&reversalCount); err != nil {
				return err
			}
			if reversalCount != 0 { return ErrExistingReversal }
			if paidMinor == 0 { return nil }

			rows, err := tx.QueryContext(ctx, `
				SELECT id,mode,amount_minor,currency FROM payments
				WHERE order_id=? AND direction='in' AND status='captured' ORDER BY created_at,id`, order.ID)
			if err != nil { return err }
			type capture struct { id, mode, currency string; amount int64 }
			var captures []capture
			var capturedTotal int64
			for rows.Next() {
				var c capture
				if err := rows.Scan(&c.id, &c.mode, &c.amount, &c.currency); err != nil { rows.Close(); return err }
				captures = append(captures, c)
				capturedTotal += c.amount
			}
			if err := rows.Close(); err != nil { return err }
			if err := rows.Err(); err != nil { return err }
			if capturedTotal != paidMinor { return fmt.Errorf("captured payment mismatch: captured=%d net=%d", capturedTotal, paidMinor) }

			for _, c := range captures {
				reference := c.id
				if _, _, err := s.payments.CreateRefundTx(ctx, tx, order.ID, payments.CreateInput{
					ClientPaymentID: "refund:" + order.ID + ":" + c.id,
					Mode: c.mode, Direction: "out", AmountMinor: c.amount, Currency: c.currency,
					Status: "refunded", Reference: &reference,
				}); err != nil { return err }
			}
			return nil
		},
		func(ctx context.Context, tx *sql.Tx, order orders.Order, _ int64) error {
			return s.inventory.ApplySaleReturnTx(ctx, tx, order)
		},
		func(ctx context.Context, tx *sql.Tx, order orders.Order, _ int64) error {
			return s.outbox.ApplySaleReturnedActorsTx(ctx, tx, order, refundedByUserID, approvedByUserID, reason)
		},
	)
}
