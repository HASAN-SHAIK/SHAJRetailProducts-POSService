package payments

import (
    "context"
    "database/sql"
    "errors"
    "testing"
)

func TestPaymentCreationWritesOneRecordedEvent(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-event", "client-order-event", 5000)
    service := New(db)
    ctx := context.Background()

    first, _, err := service.Create(ctx, "ord-event", CreateInput{ClientPaymentID:"payment-event-1", Mode:"cash", AmountMinor:5000, Status:"captured"})
    if err != nil { t.Fatal(err) }
    if _, _, err := service.Create(ctx, "ord-event", CreateInput{ClientPaymentID:"payment-event-1", Mode:"cash", AmountMinor:5000, Status:"captured"}); err != nil { t.Fatal(err) }

    var count int
    var eventType, aggregateType, aggregateID, orderingKey string
    if err := db.SQL().QueryRow(`SELECT COUNT(*),event_type,aggregate_type,aggregate_id,ordering_key FROM outbox_events WHERE event_type='payment.recorded' GROUP BY event_type,aggregate_type,aggregate_id,ordering_key`).Scan(&count,&eventType,&aggregateType,&aggregateID,&orderingKey); err != nil { t.Fatal(err) }
    if count != 1 || eventType != "payment.recorded" || aggregateType != "payment" || aggregateID != first.ID || orderingKey != "sales_order:ord-event" {
        t.Fatalf("unexpected payment event count=%d type=%s aggregate=%s/%s ordering=%s", count,eventType,aggregateType,aggregateID,orderingKey)
    }
}

func TestPaymentAndOutboxRollbackTogether(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-rollback", "client-order-rollback", 5000)
    service := New(db)
    service.SetRecordedHook(func(context.Context, *sql.Tx, Payment, Summary) error { return errors.New("forced outbox failure") })

    if _, _, err := service.Create(context.Background(), "ord-rollback", CreateInput{ClientPaymentID:"payment-rollback", Mode:"cash", AmountMinor:5000, Status:"captured"}); err == nil {
        t.Fatal("expected payment creation to roll back")
    }
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM payments WHERE order_id=?`, "ord-rollback", 0)
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM outbox_events WHERE ordering_key='sales_order:'||?`, "ord-rollback", 0)
    if got := orderVersion(t, db, "ord-rollback"); got != 1 { t.Fatalf("order version=%d want=1", got) }
}
