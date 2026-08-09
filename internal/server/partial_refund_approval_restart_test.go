package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
)

func TestManagerApprovedPartialRefundConsumesApprovalAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pos.db")

	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate before partial refund: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at)
		VALUES('product-partial-approved-restart','Partial Approved Restart Product','unit',1,0,1,1,?)`, now); err != nil {
		db.Close()
		t.Fatalf("insert product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO sales_orders(
			id,client_order_id,store_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,
			source,version,completed_at,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,'pos',2,?,?,?)`,
		"ord-partial-approved-restart", "client-partial-approved-restart", "store-1", "paid", "INR", 10000, 0, 0, 10000, now, now, now); err != nil {
		db.Close()
		t.Fatalf("insert order: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO sales_order_items(
			id,order_id,line_no,product_id,product_name,quantity_milli,unit_price_minor,discount_minor,tax_minor,line_total_minor,created_at
		) VALUES('item-partial-approved-restart','ord-partial-approved-restart',1,'product-partial-approved-restart','Partial Approved Restart Product',1000,10000,0,0,10000,?)`, now); err != nil {
		db.Close()
		t.Fatalf("insert item: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at)
		VALUES('store-1','product-partial-approved-restart',4000,0,2,?)`, now); err != nil {
		db.Close()
		t.Fatalf("insert inventory balance: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inventory_movements(
			id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at
		) VALUES('issue-partial-approved-restart','store-1','product-partial-approved-restart','sale_issue',-1000,'sale_order','ord-partial-approved-restart','item-partial-approved-restart',4000,?,?)`, now, now); err != nil {
		db.Close()
		t.Fatalf("insert sale issue: %v", err)
	}

	paymentService := payments.New(db)
	if _, _, err := paymentService.Create(ctx, "ord-partial-approved-restart", payments.CreateInput{
		ClientPaymentID: "capture-partial-approved-restart",
		Mode:            "cash",
		AmountMinor:     10000,
		Currency:        "INR",
		Status:          "captured",
	}); err != nil {
		db.Close()
		t.Fatalf("capture payment: %v", err)
	}

	token := "partial-approved-restart-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSRefund)

	s := &Server{
		db:        db,
		orders:    orders.New(db, nil),
		payments:  paymentService,
		inventory: inventory.New(db),
	}
	cashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-partial-approved-restart/refund", strings.NewReader(`{"reason":"cashier reason","return_id":"ret-partial-approved-restart","lines":[{"order_item_id":"item-partial-approved-restart","quantity_milli":250}]}`))
	req.SetPathValue("id", "ord-partial-approved-restart")
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleOrderRefund(res, req)
	if res.Code != http.StatusOK {
		db.Close()
		t.Fatalf("approved partial refund status=%d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"refunded_by_user_id":"manager-1"`) || !strings.Contains(res.Body.String(), `"return_id":"ret-partial-approved-restart"`) {
		db.Close()
		t.Fatalf("approved partial refund lost manager or operation identity body=%s", res.Body.String())
	}
	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err == nil {
		db.Close()
		t.Fatal("successful partial refund left one-time approval reusable before restart")
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("migrate after restart: %v", err)
	}

	var status, approver, reason string
	var version int
	if err := reopened.SQL().QueryRowContext(ctx, `
		SELECT status,version,approved_by_user_id,approval_reason
		FROM sales_orders WHERE id=?`, "ord-partial-approved-restart").Scan(&status, &version, &approver, &reason); err != nil {
		t.Fatal(err)
	}
	if status == "returned" || approver != "manager-1" || reason != "approved sensitive action" {
		t.Fatalf("restart partial order facts status=%s version=%d approver=%s reason=%s", status, version, approver, reason)
	}

	var ledger, outbound, returnMovements, partialEvents, returnedEvents int
	var onHand int64
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_partial_returns WHERE id='ret-partial-approved-restart' AND order_id='ord-partial-approved-restart' AND approved_by_user_id='manager-1' AND reason='approved sensitive action' AND refund_minor=2500`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id='ord-partial-approved-restart' AND direction='out' AND status='refunded' AND amount_minor=2500`).Scan(&outbound); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='item-partial-approved-restart' AND movement_type='sale_return' AND quantity_delta_milli=250`).Scan(&returnMovements); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-partial-approved-restart'`).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-partial-approved-restart' AND event_type='sale.partial_returned'`).Scan(&partialEvents); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-partial-approved-restart' AND event_type='sale.returned'`).Scan(&returnedEvents); err != nil {
		t.Fatal(err)
	}
	if ledger != 1 || outbound != 1 || returnMovements != 1 || onHand != 4250 || partialEvents != 1 || returnedEvents != 0 {
		t.Fatalf("restart partial refund facts ledger=%d outbound=%d returns=%d on_hand=%d partial_events=%d returned_events=%d", ledger, outbound, returnMovements, onHand, partialEvents, returnedEvents)
	}

	restartedPaymentService := payments.New(reopened)
	restarted := &Server{
		db:        reopened,
		orders:    orders.New(reopened, nil),
		payments:  restartedPaymentService,
		inventory: inventory.New(reopened),
	}
	if _, err := restarted.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err == nil {
		t.Fatal("consumed partial-refund approval became reusable after restart")
	}

	replay := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-partial-approved-restart/refund", strings.NewReader(`{"reason":"retry","return_id":"ret-partial-approved-restart","lines":[{"order_item_id":"item-partial-approved-restart","quantity_milli":250}]}`))
	replay.SetPathValue("id", "ord-partial-approved-restart")
	replay.Header.Set("X-POS-Approval-Token", token)
	replay = replay.WithContext(context.WithValue(replay.Context(), authContextKey{}, cashier))
	replayRes := httptest.NewRecorder()
	restarted.handleOrderRefund(replayRes, replay)
	if replayRes.Code != http.StatusForbidden || !strings.Contains(replayRes.Body.String(), "manager_approval_required") {
		t.Fatalf("restarted partial replay status=%d body=%s", replayRes.Code, replayRes.Body.String())
	}

	var versionAfterReplay, ledgerAfterReplay, outboundAfterReplay, returnMovementsAfterReplay, partialEventsAfterReplay int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT version FROM sales_orders WHERE id=?`, "ord-partial-approved-restart").Scan(&versionAfterReplay); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_partial_returns WHERE id='ret-partial-approved-restart'`).Scan(&ledgerAfterReplay); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id='ord-partial-approved-restart' AND direction='out'`).Scan(&outboundAfterReplay); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='item-partial-approved-restart' AND movement_type='sale_return'`).Scan(&returnMovementsAfterReplay); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-partial-approved-restart' AND event_type='sale.partial_returned'`).Scan(&partialEventsAfterReplay); err != nil {
		t.Fatal(err)
	}
	if versionAfterReplay != version || ledgerAfterReplay != 1 || outboundAfterReplay != 1 || returnMovementsAfterReplay != 1 || partialEventsAfterReplay != 1 {
		t.Fatalf("replayed partial refund mutated durable state version=%d/%d ledger=%d outbound=%d returns=%d partial_events=%d", versionAfterReplay, version, ledgerAfterReplay, outboundAfterReplay, returnMovementsAfterReplay, partialEventsAfterReplay)
	}
}
