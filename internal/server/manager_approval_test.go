package server

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func TestManagerApprovalIsBoundAndSingleUse(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }

	s := &Server{db: db}
	token := "one-time-approval-token"
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(token_hash,cashier_user_id,approver_user_id,permission,reason,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?)`,
		hash[:], "cashier-1", "manager-1", permissionPOSDiscount, "customer retention", now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil { t.Fatal(err) }

	approval, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSDiscount)
	if err != nil { t.Fatalf("first consume: %v", err) }
	if approval.ApproverUserID != "manager-1" || approval.Reason != "customer retention" {
		t.Fatalf("unexpected approval: %+v", approval)
	}
	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSDiscount); err == nil {
		t.Fatal("expected second consume to fail")
	}
}

func TestDiscountedOrderCanUseOneTimeApproval(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }

	s := &Server{db: db}
	token := "discount-approval-token"
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(token_hash,cashier_user_id,approver_user_id,permission,created_at,expires_at)
		VALUES(?,?,?,?,?,?)`,
		hash[:], "cashier-1", "manager-1", permissionPOSDiscount, now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil { t.Fatal(err) }

	nextCalled := false
	handler := s.requireOrderWrite(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		approval, ok := approvalFromContext(r.Context())
		if !ok || approval.ApproverUserID != "manager-1" { t.Fatalf("approval missing from context: %+v", approval) }
		w.WriteHeader(http.StatusNoContent)
	})

	user := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"items":[{"discount_minor":250}]}`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, user))
	res := httptest.NewRecorder()
	handler(res, req)

	if !nextCalled || res.Code != http.StatusNoContent {
		t.Fatalf("approved discount denied: called=%v status=%d body=%s", nextCalled, res.Code, res.Body.String())
	}

	replay := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"items":[{"discount_minor":250}]}`))
	replay.Header.Set("X-POS-Approval-Token", token)
	replay = replay.WithContext(context.WithValue(replay.Context(), authContextKey{}, user))
	replayRes := httptest.NewRecorder()
	handler(replayRes, replay)
	if replayRes.Code != http.StatusForbidden || !strings.Contains(replayRes.Body.String(), "manager_approval_required") {
		t.Fatalf("replayed approval status=%d body=%s", replayRes.Code, replayRes.Body.String())
	}
}

func TestVoidApprovalWrongCashierSurvivesRestartWithoutSideEffects(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pos.db")

	db, err := database.Open(ctx, path)
	if err != nil { t.Fatal(err) }
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate before wrong-cashier void: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO sales_orders(
			id,client_order_id,store_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,
			source,version,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,'pos',1,?,?)`,
		"ord-void-cashier-scope", "client-void-cashier-scope", "store-1", "confirmed", "INR", 1000, 0, 0, 1000, now, now); err != nil {
		db.Close()
		t.Fatalf("insert order: %v", err)
	}

	token := "void-wrong-cashier-restart-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSVoid)
	if _, err := db.SQL().ExecContext(ctx, `
		UPDATE pos_manager_approvals
		SET order_id=?
		WHERE cashier_user_id=? AND permission=? AND consumed_at IS NULL`,
		"ord-void-cashier-scope", "cashier-1", permissionPOSVoid); err != nil {
		db.Close()
		t.Fatalf("scope approval to order: %v", err)
	}

	s := &Server{db: db, orders: orders.New(db, nil)}
	wrongCashier := LocalUserContext{UserID: "cashier-2", Permissions: []string{permissionPOSSale}}
	wrong := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-void-cashier-scope/void", strings.NewReader(`{"reason":"wrong cashier"}`))
	wrong.SetPathValue("id", "ord-void-cashier-scope")
	wrong.Header.Set("X-POS-Approval-Token", token)
	wrong = wrong.WithContext(context.WithValue(wrong.Context(), authContextKey{}, wrongCashier))
	wrongRes := httptest.NewRecorder()
	s.handleOrderVoid(wrongRes, wrong)
	if wrongRes.Code != http.StatusForbidden || !strings.Contains(wrongRes.Body.String(), "manager_approval_required") {
		db.Close()
		t.Fatalf("wrong-cashier void status=%d body=%s", wrongRes.Code, wrongRes.Body.String())
	}

	if err := db.Close(); err != nil { t.Fatal(err) }

	reopened, err := database.Open(ctx, path)
	if err != nil { t.Fatal(err) }
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil { t.Fatalf("migrate after restart: %v", err) }

	var status string
	var version int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT status,version FROM sales_orders WHERE id=?`, "ord-void-cashier-scope").Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "confirmed" || version != 1 {
		t.Fatalf("wrong-cashier attempt mutated order status=%s version=%d", status, version)
	}

	var paymentFacts, inventoryFacts, syncFacts int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id=?`, "ord-void-cashier-scope").Scan(&paymentFacts); err != nil { t.Fatal(err) }
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE reference_id=?`, "ord-void-cashier-scope").Scan(&inventoryFacts); err != nil { t.Fatal(err) }
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id=? OR ordering_key=?`, "ord-void-cashier-scope", "sales_order:ord-void-cashier-scope").Scan(&syncFacts); err != nil { t.Fatal(err) }
	if paymentFacts != 0 || inventoryFacts != 0 || syncFacts != 0 {
		t.Fatalf("wrong-cashier attempt created side effects payments=%d inventory=%d sync=%d", paymentFacts, inventoryFacts, syncFacts)
	}

	restarted := &Server{db: reopened, orders: orders.New(reopened, nil)}
	rightfulCashier := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	intended := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ord-void-cashier-scope/void", strings.NewReader(`{"reason":"cashier text must not override manager reason"}`))
	intended.SetPathValue("id", "ord-void-cashier-scope")
	intended.Header.Set("X-POS-Approval-Token", token)
	intended = intended.WithContext(context.WithValue(intended.Context(), authContextKey{}, rightfulCashier))
	intendedRes := httptest.NewRecorder()
	restarted.handleOrderVoid(intendedRes, intended)
	if intendedRes.Code != http.StatusOK {
		t.Fatalf("rightful cashier void after restart status=%d body=%s", intendedRes.Code, intendedRes.Body.String())
	}

	if err := reopened.SQL().QueryRowContext(ctx, `SELECT status,version FROM sales_orders WHERE id=?`, "ord-void-cashier-scope").Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || version != 2 {
		t.Fatalf("rightful cashier did not consume preserved approval status=%s version=%d", status, version)
	}
	if _, err := restarted.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSVoid); err == nil {
		t.Fatal("rightful cashier void did not consume preserved one-time approval")
	}
}
