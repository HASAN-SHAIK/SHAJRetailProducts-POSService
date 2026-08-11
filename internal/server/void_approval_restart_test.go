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
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func TestManagerApprovedVoidConsumesApprovalAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pos.db")

	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate before void: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO sales_orders(
			id,client_order_id,store_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,
			source,version,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,'pos',1,?,?)`,
		"ord-void-approved-restart", "client-void-approved-restart", "store-1", "confirmed", "INR", 1000, 0, 0, 1000, now, now); err != nil {
		db.Close()
		t.Fatalf("insert order: %v", err)
	}

	token := "void-approved-restart-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSVoid)
	if _, err := db.SQL().ExecContext(ctx, `
		UPDATE pos_manager_approvals
		SET order_id=?
		WHERE cashier_user_id=? AND permission=? AND consumed_at IS NULL`,
		"ord-void-approved-restart", "cashier-1", permissionPOSVoid); err != nil {
		db.Close()
		t.Fatalf("scope approval to order: %v", err)
	}

	s := &Server{db: db, orders: orders.New(db, nil)}
	cashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-void-approved-restart/void", strings.NewReader(`{"reason":"cashier supplied reason must not override manager approval"}`))
	req.SetPathValue("id", "ord-void-approved-restart")
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleOrderVoid(res, req)
	if res.Code != http.StatusOK {
		db.Close()
		t.Fatalf("approved void status=%d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"voided_by_user_id":"manager-1"`) {
		db.Close()
		t.Fatalf("approved void lost manager identity body=%s", res.Body.String())
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSVoid); err == nil {
		db.Close()
		t.Fatal("successful void left one-time approval reusable before restart")
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
		FROM sales_orders WHERE id=?`, "ord-void-approved-restart").Scan(&status, &version, &approver, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || version != 2 || approver != "manager-1" || reason != "approved sensitive action" {
		t.Fatalf("restart facts status=%s version=%d approver=%s reason=%s", status, version, approver, reason)
	}

	var paymentFacts, inventoryFacts, syncFacts int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id=?`, "ord-void-approved-restart").Scan(&paymentFacts); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE reference_id=?`, "ord-void-approved-restart").Scan(&inventoryFacts); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id=? OR ordering_key=?`, "ord-void-approved-restart", "sales_order:ord-void-approved-restart").Scan(&syncFacts); err != nil {
		t.Fatal(err)
	}
	if paymentFacts != 0 || inventoryFacts != 0 || syncFacts != 0 {
		t.Fatalf("pre-completion void escaped local boundary payments=%d inventory=%d sync=%d", paymentFacts, inventoryFacts, syncFacts)
	}

	restarted := &Server{db: reopened, orders: orders.New(reopened, nil)}
	if _, err := restarted.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSVoid); err == nil {
		t.Fatal("consumed void approval became reusable after restart")
	}

	replay := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-void-approved-restart/void", strings.NewReader(`{"reason":"retry"}`))
	replay.SetPathValue("id", "ord-void-approved-restart")
	replay.Header.Set("X-POS-Approval-Token", token)
	replay = replay.WithContext(context.WithValue(replay.Context(), authContextKey{}, cashier))
	replayRes := httptest.NewRecorder()
	restarted.handleOrderVoid(replayRes, replay)
	if replayRes.Code != http.StatusForbidden || !strings.Contains(replayRes.Body.String(), "manager_approval_required") {
		t.Fatalf("restarted replay status=%d body=%s", replayRes.Code, replayRes.Body.String())
	}

	var versionAfterReplay int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT version FROM sales_orders WHERE id=?`, "ord-void-approved-restart").Scan(&versionAfterReplay); err != nil {
		t.Fatal(err)
	}
	if versionAfterReplay != 2 {
		t.Fatalf("replayed HTTP void changed order version=%d want=2", versionAfterReplay)
	}

	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id=?`, "ord-void-approved-restart").Scan(&paymentFacts); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE reference_id=?`, "ord-void-approved-restart").Scan(&inventoryFacts); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id=? OR ordering_key=?`, "ord-void-approved-restart", "sales_order:ord-void-approved-restart").Scan(&syncFacts); err != nil {
		t.Fatal(err)
	}
	if paymentFacts != 0 || inventoryFacts != 0 || syncFacts != 0 {
		t.Fatalf("rejected restarted void replay created side effects payments=%d inventory=%d sync=%d", paymentFacts, inventoryFacts, syncFacts)
	}
}
