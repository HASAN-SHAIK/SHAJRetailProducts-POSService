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

func TestManagerApprovedRefundConsumesApprovalAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pos.db")

	db, err := database.Open(ctx, path)
	if err != nil { t.Fatal(err) }
	if err := db.Migrate(ctx); err != nil { db.Close(); t.Fatalf("migrate before refund: %v", err) }

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES('product-refund-approved-restart','Refund Approved Restart Product','unit',1,0,1,1,?)`, now); err != nil { db.Close(); t.Fatalf("insert product: %v", err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO sales_orders(id,client_order_id,store_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,source,version,completed_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'pos',2,?,?,?)`, "ord-refund-approved-restart", "client-refund-approved-restart", "store-1", "paid", "INR", 10000, 0, 0, 10000, now, now, now); err != nil { db.Close(); t.Fatalf("insert order: %v", err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO sales_order_items(id,order_id,line_no,product_id,product_name,quantity_milli,unit_price_minor,discount_minor,tax_minor,line_total_minor,created_at) VALUES('item-refund-approved-restart','ord-refund-approved-restart',1,'product-refund-approved-restart','Refund Approved Restart Product',1000,10000,0,0,10000,?)`, now); err != nil { db.Close(); t.Fatalf("insert item: %v", err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at) VALUES('store-1','product-refund-approved-restart',4000,0,2,?)`, now); err != nil { db.Close(); t.Fatalf("insert inventory balance: %v", err) }
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_movements(id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at) VALUES('issue-refund-approved-restart','store-1','product-refund-approved-restart','sale_issue',-1000,'sale_order','ord-refund-approved-restart','item-refund-approved-restart',4000,?,?)`, now, now); err != nil { db.Close(); t.Fatalf("insert sale issue: %v", err) }

	paymentService := payments.New(db)
	if _, _, err := paymentService.Create(ctx, "ord-refund-approved-restart", payments.CreateInput{ClientPaymentID: "capture-refund-approved-restart", Mode: "cash", AmountMinor: 10000, Currency: "INR", Status: "captured"}); err != nil { db.Close(); t.Fatalf("capture payment: %v", err) }

	token := "refund-approved-restart-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSRefund)
	if _, err := db.SQL().ExecContext(ctx, `UPDATE pos_manager_approvals SET order_id=?, action_scope=? WHERE cashier_user_id=? AND permission=? AND consumed_at IS NULL`, "ord-refund-approved-restart", approvalActionRefundFull, "cashier-1", permissionPOSRefund); err != nil { db.Close(); t.Fatalf("scope approval to full refund: %v", err) }

	s := &Server{db: db, orders: orders.New(db, nil), payments: paymentService, inventory: inventory.New(db)}
	cashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-refund-approved-restart/refund", strings.NewReader(`{"reason":"cashier supplied reason must not override manager approval"}`))
	req.SetPathValue("id", "ord-refund-approved-restart")
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()
	s.handleOrderRefund(res, req)
	if res.Code != http.StatusOK { db.Close(); t.Fatalf("approved refund status=%d body=%s", res.Code, res.Body.String()) }
	if !strings.Contains(res.Body.String(), `"refunded_by_user_id":"manager-1"`) { db.Close(); t.Fatalf("approved refund lost manager identity body=%s", res.Body.String()) }
	if _, err := s.consumeManagerApprovalForRefundAction(ctx, token, "cashier-1", "ord-refund-approved-restart", approvalActionRefundFull); err == nil { db.Close(); t.Fatal("successful refund left one-time approval reusable before restart") }
	if err := db.Close(); err != nil { t.Fatal(err) }

	reopened, err := database.Open(ctx, path)
	if err != nil { t.Fatal(err) }
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil { t.Fatalf("migrate after restart: %v", err) }

	var status, approver, reason string
	var version int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT status,version,approved_by_user_id,approval_reason FROM sales_orders WHERE id=?`, "ord-refund-approved-restart").Scan(&status, &version, &approver, &reason); err != nil { t.Fatal(err) }
	if status != "returned" || approver != "manager-1" || reason != "approved sensitive action" { t.Fatalf("restart order facts status=%s version=%d approver=%s reason=%s", status, version, approver, reason) }

	var outbound, returnMovements, returnedEvents int
	var onHand int64
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id='ord-refund-approved-restart' AND direction='out' AND status='refunded' AND amount_minor=10000`).Scan(&outbound); err != nil { t.Fatal(err) }
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='item-refund-approved-restart' AND movement_type='sale_return' AND quantity_delta_milli=1000`).Scan(&returnMovements); err != nil { t.Fatal(err) }
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-refund-approved-restart'`).Scan(&onHand); err != nil { t.Fatal(err) }
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-approved-restart' AND event_type='sale.returned'`).Scan(&returnedEvents); err != nil { t.Fatal(err) }
	if outbound != 1 || returnMovements != 1 || onHand != 5000 || returnedEvents != 1 { t.Fatalf("restart refund facts outbound=%d returns=%d on_hand=%d returned_events=%d", outbound, returnMovements, onHand, returnedEvents) }

	restartedPaymentService := payments.New(reopened)
	restarted := &Server{db: reopened, orders: orders.New(reopened, nil), payments: restartedPaymentService, inventory: inventory.New(reopened)}
	if _, err := restarted.consumeManagerApprovalForRefundAction(ctx, token, "cashier-1", "ord-refund-approved-restart", approvalActionRefundFull); err == nil { t.Fatal("consumed refund approval became reusable after restart") }

	replay := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-refund-approved-restart/refund", strings.NewReader(`{"reason":"retry"}`))
	replay.SetPathValue("id", "ord-refund-approved-restart")
	replay.Header.Set("X-POS-Approval-Token", token)
	replay = replay.WithContext(context.WithValue(replay.Context(), authContextKey{}, cashier))
	replayRes := httptest.NewRecorder()
	restarted.handleOrderRefund(replayRes, replay)
	if replayRes.Code != http.StatusForbidden || !strings.Contains(replayRes.Body.String(), "manager_approval_required") { t.Fatalf("restarted replay status=%d body=%s", replayRes.Code, replayRes.Body.String()) }

	var versionAfterReplay, outboundAfterReplay, returnMovementsAfterReplay, returnedEventsAfterReplay int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT version FROM sales_orders WHERE id=?`, "ord-refund-approved-restart").Scan(&versionAfterReplay); err != nil { t.Fatal(err) }
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id='ord-refund-approved-restart' AND direction='out'`).Scan(&outboundAfterReplay); err != nil { t.Fatal(err) }
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='item-refund-approved-restart' AND movement_type='sale_return'`).Scan(&returnMovementsAfterReplay); err != nil { t.Fatal(err) }
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-approved-restart' AND event_type='sale.returned'`).Scan(&returnedEventsAfterReplay); err != nil { t.Fatal(err) }
	if versionAfterReplay != version || outboundAfterReplay != 1 || returnMovementsAfterReplay != 1 || returnedEventsAfterReplay != 1 { t.Fatalf("replayed refund mutated durable state version=%d/%d outbound=%d returns=%d returned_events=%d", versionAfterReplay, version, outboundAfterReplay, returnMovementsAfterReplay, returnedEventsAfterReplay) }
}