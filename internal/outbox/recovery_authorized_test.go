package outbox

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestApplyAuthorizedRecoveryConsumesCentralAuthorizationExactlyOnce(t *testing.T) {
    db := testutil.OpenDatabase(t)
    ctx := context.Background()
    now := time.Now().UTC().Format(time.RFC3339Nano)
    orderID := "ord-recovery-single-use"
    key := "sales_order:" + orderID
    eventID := "evt-recovery-single-use"
    if _, err := db.SQL().ExecContext(ctx, `
        INSERT INTO outbox_events(
            id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,
            ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at,last_error
        ) VALUES(?,?,?,1,'payment.recorded',1,?,'{}','{}','dead_letter',12,?,?,?)`,
        eventID, "payment", "pay-recovery", key, now, now, "poisoned"); err != nil {
        t.Fatal(err)
    }

    auth := RecoveryAuthorization{
        RecoveryID: "recovery-single-use-1",
        EventID: eventID,
        OrderingKey: key,
        OrderID: orderID,
        ApprovedByUserID: "manager-central-1",
        Reason: "reviewed poisoned refund payment",
    }
    svc := New(db)
    if err := svc.ApplyAuthorizedRecovery(ctx, auth); err != nil {
        t.Fatal(err)
    }

    var status, approvedBy, reason string
    var attempts, auditCount int
    if err := db.SQL().QueryRowContext(ctx, `SELECT status,attempt_count FROM outbox_events WHERE id=?`, eventID).Scan(&status, &attempts); err != nil {
        t.Fatal(err)
    }
    if status != "pending" || attempts != 0 {
        t.Fatalf("recovered event status=%s attempts=%d", status, attempts)
    }
    if err := db.SQL().QueryRowContext(ctx, `
        SELECT COUNT(*),MAX(approved_by_user_id),MAX(reason)
        FROM pos_sync_recoveries WHERE recovery_id=?`, auth.RecoveryID).Scan(&auditCount, &approvedBy, &reason); err != nil {
        t.Fatal(err)
    }
    if auditCount != 1 || approvedBy != auth.ApprovedByUserID || reason != auth.Reason {
        t.Fatalf("recovery audit count=%d approved_by=%s reason=%s", auditCount, approvedBy, reason)
    }

    if _, err := db.SQL().ExecContext(ctx, `UPDATE outbox_events SET status='dead_letter',attempt_count=12,last_error='poisoned again' WHERE id=?`, eventID); err != nil {
        t.Fatal(err)
    }
    if err := svc.ApplyAuthorizedRecovery(ctx, auth); !errors.Is(err, ErrRecoveryAlreadyConsumed) {
        t.Fatalf("replayed authorization error=%v want=%v", err, ErrRecoveryAlreadyConsumed)
    }
    if err := db.SQL().QueryRowContext(ctx, `SELECT status,attempt_count FROM outbox_events WHERE id=?`, eventID).Scan(&status, &attempts); err != nil {
        t.Fatal(err)
    }
    if status != "dead_letter" || attempts != 12 {
        t.Fatalf("replayed authorization mutated event status=%s attempts=%d", status, attempts)
    }
}

func TestApplyAuthorizedRecoveryDoesNotConsumeGrantWhenTargetCannotBeRecovered(t *testing.T) {
    db := testutil.OpenDatabase(t)
    ctx := context.Background()
    auth := RecoveryAuthorization{
        RecoveryID: "recovery-missing-target",
        EventID: "evt-missing",
        OrderingKey: "sales_order:ord-missing",
        OrderID: "ord-missing",
        ApprovedByUserID: "manager-central-1",
        Reason: "reviewed missing event",
    }

    err := New(db).ApplyAuthorizedRecovery(ctx, auth)
    if !errors.Is(err, ErrDeadLetterNotFound) {
        t.Fatalf("missing target error=%v want=%v", err, ErrDeadLetterNotFound)
    }
    var count int
    if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pos_sync_recoveries WHERE recovery_id=?`, auth.RecoveryID).Scan(&count); err != nil {
        t.Fatal(err)
    }
    if count != 0 {
        t.Fatalf("failed recovery consumed authorization rows=%d", count)
    }
}
