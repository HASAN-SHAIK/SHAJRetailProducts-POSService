package payments

import (
    "context"
    "errors"
    "path/filepath"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestDuplicateClientPaymentIsSideEffectFree(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-1", "client-order-1", 10000)

    service := New(db)
    ctx := context.Background()
    first, firstSummary, err := service.Create(ctx, "ord-1", CreateInput{
        ClientPaymentID: "client-pay-1",
        Mode: "cash",
        AmountMinor: 4000,
        Currency: "INR",
        Status: "captured",
    })
    if err != nil { t.Fatal(err) }
    if firstSummary.OrderStatus != "partially_paid" || firstSummary.PaidMinor != 4000 || firstSummary.BalanceMinor != 6000 {
        t.Fatalf("unexpected first summary: %#v", firstSummary)
    }

    versionAfterFirst := orderVersion(t, db, "ord-1")
    if versionAfterFirst != 2 { t.Fatalf("version after first payment=%d want=2", versionAfterFirst) }

    second, secondSummary, err := service.Create(ctx, "ord-1", CreateInput{
        ClientPaymentID: "client-pay-1",
        Mode: "cash",
        AmountMinor: 4000,
        Currency: "INR",
        Status: "captured",
    })
    if err != nil { t.Fatal(err) }
    if second.ID != first.ID { t.Fatalf("duplicate retry created different payment: first=%s second=%s", first.ID, second.ID) }
    if secondSummary != firstSummary { t.Fatalf("duplicate summary changed: first=%#v second=%#v", firstSummary, secondSummary) }

    if got := orderVersion(t, db, "ord-1"); got != versionAfterFirst {
        t.Fatalf("duplicate retry changed order version: got=%d want=%d", got, versionAfterFirst)
    }
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM payments WHERE order_id=?`, "ord-1", 1)
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM payment_snapshots WHERE payment_id=?`, first.ID, 1)
}

func TestPaymentStateTransitionsAndRefund(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-2", "client-order-2", 10000)

    service := New(db)
    ctx := context.Background()

    _, partial, err := service.Create(ctx, "ord-2", CreateInput{ClientPaymentID:"p-1", Mode:"cash", AmountMinor:4000, Status:"captured"})
    if err != nil { t.Fatal(err) }
    if partial.OrderStatus != "partially_paid" || partial.PaidMinor != 4000 || partial.BalanceMinor != 6000 {
        t.Fatalf("unexpected partial summary: %#v", partial)
    }

    _, paid, err := service.Create(ctx, "ord-2", CreateInput{ClientPaymentID:"p-2", Mode:"upi", AmountMinor:6000, Status:"captured"})
    if err != nil { t.Fatal(err) }
    if paid.OrderStatus != "paid" || paid.PaidMinor != 10000 || paid.BalanceMinor != 0 {
        t.Fatalf("unexpected paid summary: %#v", paid)
    }

    _, refunded, err := service.Create(ctx, "ord-2", CreateInput{ClientPaymentID:"p-3", Mode:"cash", Direction:"out", AmountMinor:2500, Status:"refunded"})
    if err != nil { t.Fatal(err) }
    if refunded.OrderStatus != "partially_paid" || refunded.PaidMinor != 7500 || refunded.BalanceMinor != 2500 {
        t.Fatalf("unexpected refund summary: %#v", refunded)
    }
}

func TestClientPaymentIDCannotMoveAcrossOrders(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-a", "client-order-a", 5000)
    seedPaymentOrder(t, db, "ord-b", "client-order-b", 5000)

    service := New(db)
    ctx := context.Background()
    if _, _, err := service.Create(ctx, "ord-a", CreateInput{ClientPaymentID:"same-client-payment", Mode:"cash", AmountMinor:5000, Status:"captured"}); err != nil {
        t.Fatal(err)
    }
    if _, _, err := service.Create(ctx, "ord-b", CreateInput{ClientPaymentID:"same-client-payment", Mode:"cash", AmountMinor:5000, Status:"captured"}); !errors.Is(err, ErrInvalidPayment) {
        t.Fatalf("cross-order client payment reuse error=%v want=%v", err, ErrInvalidPayment)
    }
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM payments WHERE client_payment_id=?`, "same-client-payment", 1)
}

func TestRejectedPaymentsDoNotMutateOrder(t *testing.T) {
    db := openPaymentsDB(t)
    defer db.Close()
    seedPaymentOrder(t, db, "ord-invalid", "client-order-invalid", 5000)
    service := New(db)
    ctx := context.Background()

    before := orderVersion(t, db, "ord-invalid")
    cases := []CreateInput{
        {ClientPaymentID:"bad-amount", Mode:"cash", AmountMinor:0, Status:"captured"},
        {ClientPaymentID:"bad-mode", Mode:"crypto", AmountMinor:1000, Status:"captured"},
        {ClientPaymentID:"bad-currency", Mode:"cash", AmountMinor:1000, Currency:"USD", Status:"captured"},
    }
    for _, input := range cases {
        if _, _, err := service.Create(ctx, "ord-invalid", input); !errors.Is(err, ErrInvalidPayment) {
            t.Fatalf("input %#v error=%v want=%v", input, err, ErrInvalidPayment)
        }
    }
    if got := orderVersion(t, db, "ord-invalid"); got != before {
        t.Fatalf("invalid payment changed order version: got=%d want=%d", got, before)
    }
    assertPaymentCount(t, db, `SELECT COUNT(*) FROM payments WHERE order_id=?`, "ord-invalid", 0)
}

func openPaymentsDB(t *testing.T) *database.DB {
    t.Helper()
    db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "payments.db"))
    if err != nil { t.Fatal(err) }
    if err := db.Migrate(context.Background()); err != nil { db.Close(); t.Fatal(err) }
    return db
}

func seedPaymentOrder(t *testing.T, db *database.DB, id, clientID string, total int64) {
    t.Helper()
    now := time.Now().UTC().Format(time.RFC3339Nano)
    _, err := db.SQL().Exec(`INSERT INTO sales_orders(
        id,client_order_id,store_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,
        source,version,created_at,updated_at)
        VALUES(?,?,?,'confirmed','INR',?,0,0,?,'pos',1,?,?)`, id, clientID, "store-1", total, total, now, now)
    if err != nil { t.Fatal(err) }
}

func orderVersion(t *testing.T, db *database.DB, orderID string) int {
    t.Helper()
    var version int
    if err := db.SQL().QueryRow(`SELECT version FROM sales_orders WHERE id=?`, orderID).Scan(&version); err != nil { t.Fatal(err) }
    return version
}

func assertPaymentCount(t *testing.T, db *database.DB, query, arg string, want int) {
    t.Helper()
    var got int
    if err := db.SQL().QueryRow(query, arg).Scan(&got); err != nil { t.Fatal(err) }
    if got != want { t.Fatalf("count mismatch for %q: got=%d want=%d", query, got, want) }
}
