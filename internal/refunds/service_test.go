package refunds

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
)

func openRefundDB(t *testing.T) *database.DB {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	if err := db.Migrate(ctx); err != nil { db.Close(); t.Fatal(err) }
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedCompletedSale(t *testing.T, db *database.DB, withBalance bool) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO sales_orders(id,client_order_id,store_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,source,version,completed_at,created_at,updated_at)
		VALUES('ord-refund-full','client-refund-full','store-1','paid','INR',10000,0,0,10000,'pos',2,?,?,?)`, now, now, now); err != nil { t.Fatal(err) }
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO sales_order_items(id,order_id,line_no,product_id,product_name,quantity_milli,unit_price_minor,discount_minor,tax_minor,line_total_minor,created_at)
		VALUES('item-refund-full','ord-refund-full',1,'product-1','Refund Product',1000,10000,0,0,10000,?)`, now); err != nil { t.Fatal(err) }
	if withBalance {
		if _, err := db.SQL().ExecContext(ctx, `INSERT INTO inventory_balances(store_id,product_id,on_hand_milli,reserved_milli,version,updated_at) VALUES('store-1','product-1',4000,0,2,?)`, now); err != nil { t.Fatal(err) }
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inventory_movements(id,store_id,product_id,movement_type,quantity_delta_milli,reference_type,reference_id,order_item_id,balance_after_milli,occurred_at,created_at)
		VALUES('issue-refund-full','store-1','product-1','sale_issue',-1000,'sale_order','ord-refund-full','item-refund-full',4000,?,?)`, now, now); err != nil { t.Fatal(err) }
}

func newRefundService(db *database.DB) (*Service, *payments.Service) {
	paymentService := payments.New(db)
	return New(db, orders.New(db, nil), paymentService, inventory.New(db)), paymentService
}

func TestRefundFullSaleCommitsMoneyInventoryStateAuditAndEventTogether(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	svc, paymentService := newRefundService(db)
	if _, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{ClientPaymentID:"capture-1", Mode:"cash", AmountMinor:10000, Currency:"INR", Status:"captured"}); err != nil { t.Fatal(err) }

	order, err := svc.RefundFullSale(ctx, "ord-refund-full", "manager-1", "customer returned goods")
	if err != nil { t.Fatal(err) }
	if order.Status != "returned" { t.Fatalf("status=%s want=returned", order.Status) }

	var status, approver, reason string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status,approved_by_user_id,approval_reason FROM sales_orders WHERE id='ord-refund-full'`).Scan(&status,&approver,&reason); err != nil { t.Fatal(err) }
	if status != "returned" || approver != "manager-1" || reason != "customer returned goods" { t.Fatalf("audit mismatch %s %s %s", status, approver, reason) }

	var outbound, returns, returnedEvents int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id='ord-refund-full' AND direction='out' AND status='refunded' AND amount_minor=10000`).Scan(&outbound); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE order_item_id='item-refund-full' AND movement_type='sale_return'`).Scan(&returns); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-full' AND event_type='sale.returned'`).Scan(&returnedEvents); err != nil { t.Fatal(err) }
	if outbound != 1 || returns != 1 || returnedEvents != 1 { t.Fatalf("outbound=%d returns=%d sale.returned=%d", outbound, returns, returnedEvents) }
	var onHand int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT on_hand_milli FROM inventory_balances WHERE store_id='store-1' AND product_id='product-1'`).Scan(&onHand); err != nil { t.Fatal(err) }
	if onHand != 5000 { t.Fatalf("on_hand=%d want=5000", onHand) }
}

func TestRefundFullSaleRollsBackPaymentWhenInventoryCompensationFails(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, false)
	svc, paymentService := newRefundService(db)
	if _, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{ClientPaymentID:"capture-1", Mode:"card", AmountMinor:10000, Currency:"INR", Status:"captured"}); err != nil { t.Fatal(err) }

	if _, err := svc.RefundFullSale(ctx, "ord-refund-full", "manager-1", "return"); err == nil { t.Fatal("expected inventory compensation failure") }
	var outbound, returnedEvents int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id='ord-refund-full' AND direction='out'`).Scan(&outbound); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-full' AND event_type='sale.returned'`).Scan(&returnedEvents); err != nil { t.Fatal(err) }
	if outbound != 0 || returnedEvents != 0 { t.Fatalf("rollback leaked outbound=%d event=%d", outbound, returnedEvents) }
	var status string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM sales_orders WHERE id='ord-refund-full'`).Scan(&status); err != nil { t.Fatal(err) }
	if status == "returned" { t.Fatal("order returned despite failed atomic refund") }
}
