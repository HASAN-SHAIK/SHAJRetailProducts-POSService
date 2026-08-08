package payments

import (
    "context"
    "errors"
    "testing"
)

func TestCreateRefundTxCommitsWithPaymentEventAndIsIdempotent(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-refund", "client-order-refund", 10000)
    service := New(db)
    ctx := context.Background()

    if _, _, err := service.Create(ctx, "ord-refund", CreateInput{
        ClientPaymentID: "capture-1", Mode: "cash", AmountMinor: 10000, Status: "captured",
    }); err != nil { t.Fatal(err) }

    tx, err := db.SQL().BeginTx(ctx, nil)
    if err != nil { t.Fatal(err) }
    first, firstSummary, err := service.CreateRefundTx(ctx, tx, "ord-refund", CreateInput{
        ClientPaymentID: "refund-1", Mode: "cash", AmountMinor: 10000,
    })
    if err != nil { tx.Rollback(); t.Fatal(err) }
    if first.Direction != "out" || first.Status != "refunded" {
        tx.Rollback(); t.Fatalf("unexpected refund payment: %#v", first)
    }
    if firstSummary.PaidMinor != 0 || firstSummary.BalanceMinor != 10000 {
        tx.Rollback(); t.Fatalf("unexpected refund summary: %#v", firstSummary)
    }

    versionAfterFirst := 0
    if err := tx.QueryRowContext(ctx, `SELECT version FROM sales_orders WHERE id=?`, "ord-refund").Scan(&versionAfterFirst); err != nil {
        tx.Rollback(); t.Fatal(err)
    }
    second, secondSummary, err := service.CreateRefundTx(ctx, tx, "ord-refund", CreateInput{
        ClientPaymentID: "refund-1", Mode: "cash", AmountMinor: 10000,
    })
    if err != nil { tx.Rollback(); t.Fatal(err) }
    if second.ID != first.ID || secondSummary != firstSummary {
        tx.Rollback(); t.Fatalf("refund replay changed result: first=%#v/%#v second=%#v/%#v", first, firstSummary, second, secondSummary)
    }
    var versionAfterReplay int
    if err := tx.QueryRowContext(ctx, `SELECT version FROM sales_orders WHERE id=?`, "ord-refund").Scan(&versionAfterReplay); err != nil {
        tx.Rollback(); t.Fatal(err)
    }
    if versionAfterReplay != versionAfterFirst {
        tx.Rollback(); t.Fatalf("refund replay changed order version: first=%d replay=%d", versionAfterFirst, versionAfterReplay)
    }
    if err := tx.Commit(); err != nil { t.Fatal(err) }

    assertPaymentCount(t, db, `SELECT COUNT(*) FROM payments WHERE client_payment_id=?`, "refund-1", 1)
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM payment_snapshots WHERE payment_id=?`, first.ID, 1)
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='payment' AND aggregate_id=? AND event_type='payment.recorded'`, first.ID, 1)
}

func TestCreateRefundTxRollsBackWithCallerTransaction(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-rollback", "client-order-rollback", 5000)
    service := New(db)
    ctx := context.Background()

    if _, _, err := service.Create(ctx, "ord-rollback", CreateInput{
        ClientPaymentID: "capture-rb", Mode: "upi", AmountMinor: 5000, Status: "captured",
    }); err != nil { t.Fatal(err) }
    versionBefore := orderVersion(t, db, "ord-rollback")

    tx, err := db.SQL().BeginTx(ctx, nil)
    if err != nil { t.Fatal(err) }
    refund, _, err := service.CreateRefundTx(ctx, tx, "ord-rollback", CreateInput{
        ClientPaymentID: "refund-rb", Mode: "upi", AmountMinor: 5000,
    })
    if err != nil { tx.Rollback(); t.Fatal(err) }
    if err := tx.Rollback(); err != nil { t.Fatal(err) }

    assertPaymentCount(t, db, `SELECT COUNT(*) FROM payments WHERE client_payment_id=?`, "refund-rb", 0)
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='payment' AND aggregate_id=?`, refund.ID, 0)
    if got := orderVersion(t, db, "ord-rollback"); got != versionBefore {
        t.Fatalf("rollback changed order version: got=%d want=%d", got, versionBefore)
    }
}

func TestCreateRefundTxRejectsOverRefundAndInboundDirection(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-limit", "client-order-limit", 8000)
    service := New(db)
    ctx := context.Background()
    if _, _, err := service.Create(ctx, "ord-limit", CreateInput{
        ClientPaymentID: "capture-limit", Mode: "card", AmountMinor: 6000, Status: "captured",
    }); err != nil { t.Fatal(err) }

    tx, err := db.SQL().BeginTx(ctx, nil)
    if err != nil { t.Fatal(err) }
    defer tx.Rollback()

    if _, _, err := service.CreateRefundTx(ctx, tx, "ord-limit", CreateInput{
        ClientPaymentID: "refund-too-much", Mode: "card", AmountMinor: 7000,
    }); !errors.Is(err, ErrInvalidPayment) {
        t.Fatalf("over-refund error=%v want=%v", err, ErrInvalidPayment)
    }
    if _, _, err := service.CreateRefundTx(ctx, tx, "ord-limit", CreateInput{
        ClientPaymentID: "refund-in", Mode: "card", Direction: "in", AmountMinor: 1000,
    }); !errors.Is(err, ErrInvalidPayment) {
        t.Fatalf("inbound refund error=%v want=%v", err, ErrInvalidPayment)
    }
}
