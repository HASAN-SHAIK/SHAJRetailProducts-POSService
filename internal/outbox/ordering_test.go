package outbox

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func openOrderingTestDB(t *testing.T) *database.DB {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "outbox-ordering.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertOrderingEvent(t *testing.T, db *database.DB, id, eventType, orderingKey, createdAt string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO outbox_events(
			id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
			ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at
		) VALUES(?,?,?,?,?,1,?,'{}','{}','pending',0,?,?)`,
		id, "test", id, 1, eventType, orderingKey, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
}

func TestClaimNextPreservesRefundOrderingWithoutBlockingOtherSales(t *testing.T) {
	ctx := context.Background()
	db := openOrderingTestDB(t)
	svc := New(db)
	base := time.Now().UTC().Add(-time.Minute)

	insertOrderingEvent(t, db, "evt-refund-payment", "payment.recorded", "sales_order:ord-refund", base.Format(time.RFC3339Nano))
	insertOrderingEvent(t, db, "evt-refund-inventory", "inventory.movement.recorded", "sales_order:ord-refund", base.Add(time.Millisecond).Format(time.RFC3339Nano))
	insertOrderingEvent(t, db, "evt-refund-sale", "sale.partial_returned", "sales_order:ord-refund", base.Add(2*time.Millisecond).Format(time.RFC3339Nano))
	insertOrderingEvent(t, db, "evt-other-sale", "sale.completed", "sales_order:ord-other", base.Add(3*time.Millisecond).Format(time.RFC3339Nano))

	first, err := svc.ClaimNext(ctx, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.ID != "evt-refund-payment" {
		t.Fatalf("first claim=%+v want evt-refund-payment", first)
	}
	if err := svc.MarkFailed(ctx, first.ID, "worker-a", "lost central acknowledgement"); err != nil {
		t.Fatal(err)
	}

	// The failed payment fact is now backing off. Later inventory and
	// sale.partial_returned facts for the same order must not overtake it, while
	// an unrelated order remains dispatchable.
	other, err := svc.ClaimNext(ctx, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if other == nil || other.ID != "evt-other-sale" {
		t.Fatalf("claim while refund head backs off=%+v want evt-other-sale", other)
	}
	if err := svc.MarkPublished(ctx, other.ID, "worker-a"); err != nil {
		t.Fatal(err)
	}

	blocked, err := svc.ClaimNext(ctx, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if blocked != nil {
		t.Fatalf("later same-order fact overtook failed refund head: %+v", blocked)
	}

	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET available_at=? WHERE id='evt-refund-payment'`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	retry, err := svc.ClaimNext(ctx, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if retry == nil || retry.ID != "evt-refund-payment" {
		t.Fatalf("retry claim=%+v want evt-refund-payment", retry)
	}
	if err := svc.MarkPublished(ctx, retry.ID, "worker-a"); err != nil {
		t.Fatal(err)
	}

	inventoryFact, err := svc.ClaimNext(ctx, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if inventoryFact == nil || inventoryFact.ID != "evt-refund-inventory" {
		t.Fatalf("second refund claim=%+v want evt-refund-inventory", inventoryFact)
	}
	if err := svc.MarkPublished(ctx, inventoryFact.ID, "worker-a"); err != nil {
		t.Fatal(err)
	}

	partialReturnFact, err := svc.ClaimNext(ctx, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if partialReturnFact == nil || partialReturnFact.ID != "evt-refund-sale" {
		t.Fatalf("third refund claim=%+v want evt-refund-sale", partialReturnFact)
	}
}

func TestDeadLetterRefundHeadBlocksSameOrderAcrossRestartButNotOtherSales(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "outbox-dead-letter-ordering.db")
	openDB := func() *database.DB {
		db, err := database.Open(ctx, dbPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(ctx); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		return db
	}

	db := openDB()
	base := time.Now().UTC().Add(-time.Minute)
	insertOrderingEvent(t, db, "evt-dead-refund-payment", "payment.recorded", "sales_order:ord-dead-refund", base.Format(time.RFC3339Nano))
	insertOrderingEvent(t, db, "evt-dead-refund-inventory", "inventory.movement.recorded", "sales_order:ord-dead-refund", base.Add(time.Millisecond).Format(time.RFC3339Nano))
	insertOrderingEvent(t, db, "evt-dead-refund-sale", "sale.partial_returned", "sales_order:ord-dead-refund", base.Add(2*time.Millisecond).Format(time.RFC3339Nano))
	insertOrderingEvent(t, db, "evt-independent-sale", "sale.completed", "sales_order:ord-independent", base.Add(3*time.Millisecond).Format(time.RFC3339Nano))

	// Put the refund head one attempt away from dead-lettering, then let the
	// production failure path perform the terminal transition.
	if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET attempt_count=11 WHERE id='evt-dead-refund-payment'`); err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	head, err := svc.ClaimNext(ctx, "worker-dead")
	if err != nil {
		t.Fatal(err)
	}
	if head == nil || head.ID != "evt-dead-refund-payment" {
		t.Fatalf("dead-letter head claim=%+v want evt-dead-refund-payment", head)
	}
	if err := svc.MarkFailed(ctx, head.ID, "worker-dead", "prolonged central outage"); err != nil {
		t.Fatal(err)
	}

	var status string
	var attempts int
	if err := db.SQL().QueryRowContext(ctx, `SELECT status,attempt_count FROM outbox_events WHERE id='evt-dead-refund-payment'`).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "dead_letter" || attempts != 12 {
		t.Fatalf("refund head status=%s attempts=%d want dead_letter/12", status, attempts)
	}

	// A poisoned refund ordering key must fail closed, but unrelated sales must
	// still make progress so one bad refund does not stall the whole terminal.
	independent, err := svc.ClaimNext(ctx, "worker-dead")
	if err != nil {
		t.Fatal(err)
	}
	if independent == nil || independent.ID != "evt-independent-sale" {
		t.Fatalf("claim with dead-letter refund head=%+v want evt-independent-sale", independent)
	}
	if err := svc.MarkPublished(ctx, independent.ID, "worker-dead"); err != nil {
		t.Fatal(err)
	}
	blocked, err := svc.ClaimNext(ctx, "worker-dead")
	if err != nil {
		t.Fatal(err)
	}
	if blocked != nil {
		t.Fatalf("same-order refund fact overtook dead-letter head before restart: %+v", blocked)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openDB()
	defer db.Close()
	svc = New(db)

	// Restart must not erase the fail-closed boundary or implicitly requeue the
	// poisoned refund. A future recovery mechanism must be explicit and audited.
	if err := db.SQL().QueryRowContext(ctx, `SELECT status,attempt_count FROM outbox_events WHERE id='evt-dead-refund-payment'`).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "dead_letter" || attempts != 12 {
		t.Fatalf("dead-letter refund head changed across restart status=%s attempts=%d", status, attempts)
	}
	blocked, err = svc.ClaimNext(ctx, "worker-after-restart")
	if err != nil {
		t.Fatal(err)
	}
	if blocked != nil {
		t.Fatalf("same-order refund fact overtook dead-letter head after restart: %+v", blocked)
	}
}
