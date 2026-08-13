package orders

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func openVoidTestDB(t *testing.T) (*database.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	if err := db.Migrate(ctx); err != nil { db.Close(); t.Fatalf("migrate: %v", err) }
	t.Cleanup(func() { _ = db.Close() })
	return db, ctx
}

func insertVoidTestOrder(t *testing.T, db *database.DB, ctx context.Context, id, status string, completedAt any) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO sales_orders(
			id,client_order_id,store_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,
			source,version,completed_at,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,'pos',1,?,?,?)`,
		id, "client_"+id, "store-1", status, "INR", 1000, 0, 0, 1000, completedAt, now, now)
	if err != nil { t.Fatalf("insert order: %v", err) }
}

func TestVoidWithCancelsUncompletedUnpaidOrderAndRecordsApproval(t *testing.T) {
	db, ctx := openVoidTestDB(t)
	insertVoidTestOrder(t, db, ctx, "ord-void", "confirmed", nil)

	svc := &Service{db: db}
	order, err := svc.VoidWith(ctx, "ord-void", "manager-1", "customer changed mind")
	if err != nil { t.Fatalf("void: %v", err) }
	if order.Status != "cancelled" || order.Version != 2 {
		t.Fatalf("unexpected order after void: %+v", order)
	}

	var status, approver, reason string
	var version int
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT status,version,approved_by_user_id,approval_reason FROM sales_orders WHERE id=?`,
		"ord-void").Scan(&status, &version, &approver, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || version != 2 || approver != "manager-1" || reason != "customer changed mind" {
		t.Fatalf("audit mismatch status=%s version=%d approver=%s reason=%s", status, version, approver, reason)
	}

	var movementCount int
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=?`,
		"ord-void").Scan(&movementCount); err != nil {
		t.Fatal(err)
	}
	if movementCount != 0 {
		t.Fatalf("pre-completion void created inventory movements: %d", movementCount)
	}
}

func TestVoidWithRejectsCompletedSaleAsRefundRequired(t *testing.T) {
	db, ctx := openVoidTestDB(t)
	completed := time.Now().UTC().Format(time.RFC3339Nano)
	insertVoidTestOrder(t, db, ctx, "ord-complete", "paid", completed)

	svc := &Service{db: db}
	_, err := svc.VoidWith(ctx, "ord-complete", "manager-1", "wrong item")
	if !errors.Is(err, ErrRefundRequired) {
		t.Fatalf("expected ErrRefundRequired, got %v", err)
	}
}

func TestVoidWithRejectsCapturedPayment(t *testing.T) {
	db, ctx := openVoidTestDB(t)
	insertVoidTestOrder(t, db, ctx, "ord-paid", "partially_paid", nil)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO payments(
			id,order_id,client_payment_id,mode,direction,amount_minor,currency,status,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"pay-1", "ord-paid", "client-pay-1", "cash", "in", 500, "INR", "captured", now, now)
	if err != nil { t.Fatalf("insert payment: %v", err) }

	svc := &Service{db: db}
	_, err = svc.VoidWith(ctx, "ord-paid", "manager-1", "cancel sale")
	if !errors.Is(err, ErrPaymentReversalRequired) {
		t.Fatalf("expected ErrPaymentReversalRequired, got %v", err)
	}

	var status string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM sales_orders WHERE id=?`, "ord-paid").Scan(&status); err != nil { t.Fatal(err) }
	if status != "partially_paid" {
		t.Fatalf("paid order mutated despite rejected void: %s", status)
	}
}
