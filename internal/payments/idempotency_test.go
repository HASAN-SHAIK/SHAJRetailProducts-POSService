package payments

import (
    "context"
    "errors"
    "testing"
)

func TestClientPaymentIDRejectsConflictingRetryPayload(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-conflict", "client-order-conflict", 5000)

    service := New(db)
    ctx := context.Background()
    if _, _, err := service.Create(ctx, "ord-conflict", CreateInput{
        ClientPaymentID: "client-pay-conflict",
        Mode: "cash",
        AmountMinor: 2500,
        Currency: "INR",
        Status: "captured",
    }); err != nil {
        t.Fatal(err)
    }
    version := orderVersion(t, db, "ord-conflict")

    if _, _, err := service.Create(ctx, "ord-conflict", CreateInput{
        ClientPaymentID: "client-pay-conflict",
        Mode: "cash",
        AmountMinor: 3000,
        Currency: "INR",
        Status: "captured",
    }); !errors.Is(err, ErrInvalidPayment) {
        t.Fatalf("conflicting retry error=%v want=%v", err, ErrInvalidPayment)
    }

    if got := orderVersion(t, db, "ord-conflict"); got != version {
        t.Fatalf("conflicting retry changed order version: got=%d want=%d", got, version)
    }
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM payments WHERE client_payment_id=?`, "client-pay-conflict", 1)
}
