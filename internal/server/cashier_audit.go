package server

import (
	"context"
	"database/sql"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func cashierFromRequest(rctx context.Context) (LocalUserContext, bool) {
	user, ok := localUserFromContext(rctx)
	if !ok || user.UserID == "" || user.UserID == "internal-test" {
		return LocalUserContext{}, false
	}
	return user, true
}

func (s *Server) recordOrderCreator(ctx context.Context, orderID string) error {
	user, ok := cashierFromRequest(ctx)
	if !ok {
		return nil
	}
	_, err := s.db.SQL().ExecContext(ctx,
		`UPDATE sales_orders SET created_by_user_id=COALESCE(created_by_user_id, ?) WHERE id=?`,
		user.UserID, orderID,
	)
	return err
}

func (s *Server) recordPaymentCreator(ctx context.Context, paymentID string) error {
	user, ok := cashierFromRequest(ctx)
	if !ok {
		return nil
	}
	_, err := s.db.SQL().ExecContext(ctx,
		`UPDATE payments SET created_by_user_id=COALESCE(created_by_user_id, ?) WHERE id=?`,
		user.UserID, paymentID,
	)
	return err
}

func (s *Server) cashierCompletionAuditHook(ctx context.Context) orders.CompletionHook {
	user, ok := cashierFromRequest(ctx)
	if !ok {
		return nil
	}

	return func(hookCtx context.Context, tx *sql.Tx, order orders.Order) error {
		if _, err := tx.ExecContext(hookCtx,
			`UPDATE sales_orders SET completed_by_user_id=? WHERE id=?`,
			user.UserID, order.ID,
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(hookCtx,
			`UPDATE receipts SET issued_by_user_id=? WHERE order_id=?`,
			user.UserID, order.ID,
		); err != nil {
			return err
		}

		var createdBy sql.NullString
		if err := tx.QueryRowContext(hookCtx,
			`SELECT created_by_user_id FROM sales_orders WHERE id=?`, order.ID,
		).Scan(&createdBy); err != nil {
			return err
		}

		_, err := tx.ExecContext(hookCtx, `
			UPDATE outbox_events
			SET payload_json = json_set(
				payload_json,
				'$.actor', json_object(
					'user_id', ?,
					'role', ?,
					'tenant_id', ?,
					'branch_id', NULLIF(?, '')
				),
				'$.order.created_by_user_id', CASE WHEN ? THEN ? ELSE NULL END,
				'$.order.completed_by_user_id', ?
			),
			metadata_json = json_set(
				metadata_json,
				'$.actor_user_id', ?,
				'$.actor_role', ?
			)
			WHERE aggregate_type='sales_order'
			  AND aggregate_id=?
			  AND aggregate_version=?
			  AND event_type='sale.completed'`,
			user.UserID,
			user.Role,
			user.TenantID,
			user.BranchID,
			createdBy.Valid,
			createdBy.String,
			user.UserID,
			user.UserID,
			user.Role,
			order.ID,
			order.Version,
		)
		return err
	}
}
