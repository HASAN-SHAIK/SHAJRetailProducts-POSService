package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
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

func TestPartialRefundOrderScopedApprovalLifecycleAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")
	db, err := database.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate: %v", err)
	}

	const (
		token      = "partial-refund-order-scope-lifecycle"
		cashierID  = "cashier-1"
		managerID  = "manager-1"
		approvedID = "order-approved"
		wrongID    = "order-wrong"
		itemID     = "item-approved"
		productID  = "product-approved"
		returnID   = "return-approved-250"
	)
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)

	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO catalog_products(id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at) VALUES(?,?,?,?,?,?,?,?)`, productID, "Scoped Refund Product", "unit", 1, 0, 1, 1, nowText); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO sales_orders(id,client_order_id,store_id,terminal_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,source,version,completed_at,created_at,updated_at) VALUES(?,?,?,?,'paid','INR',10000,0,0,10000,'pos',2,?,?,?)`, approvedID, "client-approved", "store-1", "terminal-1", nowText, nowText, nowText); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO sales_order_items(id,order_id,line_no,product_id,product_name,quantity_milli,unit_price_minor,discount_minor,tax_minor,line_total_minor,created_at) VALUES(?,?,1,?,?,1000,10000,0,0,10000,?)`, itemID, approvedID, productID, "Scoped Refund Product", nowText); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at) VALUES(?,?,4000,0,2,?)`, "store-1", productID, nowText); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_movements(id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, "issue-approved", "store-1", productID, "sale_issue", -1000, "sale_order", approvedID, itemID, 4000, nowText, nowText); err != nil {
		db.Close()
		t.Fatal(err)
	}

	paymentService := payments.New(db)
	capture, _, err := paymentService.Create(ctx, approvedID, payments.CreateInput{ClientPaymentID: "capture-approved", Mode: "cash", AmountMinor: 10000, Currency: "INR", Status: "captured"})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET status='published',published_at=? WHERE aggregate_type='payment' AND aggregate_id=?`, nowText, capture.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}

	hash := sha256.Sum256([]byte(token))
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(
			token_hash,cashier_user_id,approver_user_id,permission,reason,order_id,created_at,expires_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		hash[:], cashierID, managerID, permissionPOSRefund, "customer returned one quarter", approvedID,
		nowText, now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		db.Close()
		t.Fatalf("seed approval: %v", err)
	}

	newServer := func(current *database.DB) *Server {
		return &Server{
			db:        current,
			orders:    orders.New(current, nil),
			payments:  payments.New(current),
			inventory: inventory.New(current),
		}
	}
	cashier := LocalUserContext{UserID: cashierID, Permissions: []string{permissionPOSSale}}

	wrongReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+wrongID+"/refund", strings.NewReader(`{"return_id":"return-wrong","lines":[{"order_item_id":"item-wrong","quantity_milli":250}],"reason":"cashier reason"}`))
	wrongReq.SetPathValue("id", wrongID)
	wrongReq.Header.Set("X-POS-Approval-Token", token)
	wrongReq = wrongReq.WithContext(context.WithValue(wrongReq.Context(), authContextKey{}, cashier))
	wrongRes := httptest.NewRecorder()
	newServer(db).handleOrderRefund(wrongRes, wrongReq)
	if wrongRes.Code != http.StatusForbidden || !strings.Contains(wrongRes.Body.String(), "manager_approval_required") {
		db.Close()
		t.Fatalf("wrong-order partial refund status=%d body=%s", wrongRes.Code, wrongRes.Body.String())
	}

	var consumedAt sql.NullString
	if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if consumedAt.Valid {
		db.Close()
		t.Fatalf("wrong-order partial refund burned approval at %s", consumedAt.String)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before restart: %v", err)
	}

	db, err = database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate after restart: %v", err)
	}

	body := `{"return_id":"` + returnID + `","lines":[{"order_item_id":"` + itemID + `","quantity_milli":250}],"reason":"cashier reason"}`
	approvedReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+approvedID+"/refund", strings.NewReader(body))
	approvedReq.SetPathValue("id", approvedID)
	approvedReq.Header.Set("X-POS-Approval-Token", token)
	approvedReq = approvedReq.WithContext(context.WithValue(approvedReq.Context(), authContextKey{}, cashier))
	approvedRes := httptest.NewRecorder()
	newServer(db).handleOrderRefund(approvedRes, approvedReq)
	if approvedRes.Code != http.StatusOK {
		t.Fatalf("approved partial refund status=%d body=%s", approvedRes.Code, approvedRes.Body.String())
	}

	if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, hash[:]).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if !consumedAt.Valid {
		t.Fatal("approved partial refund did not consume one-time approval")
	}

	var outbound, returns, partialOps, partialItems, pendingFacts int
	var onHand int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id=? AND direction='out' AND status='refunded' AND amount_minor=2500`, approvedID).Scan(&outbound); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id=? AND movement_type='sale_return' AND quantity_delta_milli=250`, itemID).Scan(&returns); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id=?`, productID).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sale_partial_returns WHERE id=? AND order_id=? AND refund_minor=2500`, returnID, approvedID).Scan(&partialOps); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sale_partial_return_items WHERE return_id=? AND order_item_id=? AND quantity_milli=250`, returnID, itemID).Scan(&partialItems); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE ordering_key=? AND status='pending' AND event_type IN ('payment.recorded','inventory.movement.recorded','sale.partial_returned')`, "sales_order:"+approvedID).Scan(&pendingFacts); err != nil {
		t.Fatal(err)
	}
	if outbound != 1 || returns != 1 || onHand != 4250 || partialOps != 1 || partialItems != 1 || pendingFacts != 3 {
		t.Fatalf("unexpected partial refund facts outbound=%d returns=%d on_hand=%d ops=%d items=%d pending=%d", outbound, returns, onHand, partialOps, partialItems, pendingFacts)
	}

	replayReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+approvedID+"/refund", strings.NewReader(body))
	replayReq.SetPathValue("id", approvedID)
	replayReq.Header.Set("X-POS-Approval-Token", token)
	replayReq = replayReq.WithContext(context.WithValue(replayReq.Context(), authContextKey{}, cashier))
	replayRes := httptest.NewRecorder()
	newServer(db).handleOrderRefund(replayRes, replayReq)
	if replayRes.Code != http.StatusForbidden || !strings.Contains(replayRes.Body.String(), "manager_approval_required") {
		t.Fatalf("consumed approval replay status=%d body=%s", replayRes.Code, replayRes.Body.String())
	}

	var outboundAfter, returnsAfter, partialOpsAfter, partialItemsAfter, pendingAfter int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id=? AND direction='out' AND status='refunded'`, approvedID).Scan(&outboundAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id=? AND movement_type='sale_return'`, itemID).Scan(&returnsAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sale_partial_returns WHERE id=?`, returnID).Scan(&partialOpsAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sale_partial_return_items WHERE return_id=?`, returnID).Scan(&partialItemsAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE ordering_key=? AND status='pending' AND event_type IN ('payment.recorded','inventory.movement.recorded','sale.partial_returned')`, "sales_order:"+approvedID).Scan(&pendingAfter); err != nil {
		t.Fatal(err)
	}
	if outboundAfter != outbound || returnsAfter != returns || partialOpsAfter != partialOps || partialItemsAfter != partialItems || pendingAfter != pendingFacts {
		t.Fatalf("approval replay changed refund facts outbound=%d/%d returns=%d/%d ops=%d/%d items=%d/%d pending=%d/%d", outboundAfter, outbound, returnsAfter, returns, partialOpsAfter, partialOps, partialItemsAfter, partialItems, pendingAfter, pendingFacts)
	}
}
