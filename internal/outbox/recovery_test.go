package outbox

import (
    "context"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestRequeueDeadLetterOnlyReenablesApprovedHead(t *testing.T) {
    db := testutil.OpenDatabase(t)
    ctx := context.Background()
    now := time.Now().UTC().Format(time.RFC3339Nano)
    key := "sales_order:ord-recovery-1"
    inserts := []struct{ id, status string; version int }{
        {"evt-dead", "dead_letter", 1},
        {"evt-later", "pending", 2},
    }
    for _, item := range inserts {
        if _, err := db.SQL().ExecContext(ctx, `INSERT INTO outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at,last_error) VALUES(?,?,?,?,'payment.recorded',1,?,'{}','{}',?,12,?,?,?)`, item.id, "sales_order", "ord-recovery-1", item.version, key, item.status, now, now, "poisoned"); err != nil { t.Fatal(err) }
    }

    svc := New(db)
    if err := svc.RequeueDeadLetter(ctx, "evt-dead", key); err != nil { t.Fatal(err) }

    var status string
    var attempts int
    var lastError *string
    if err := db.SQL().QueryRowContext(ctx, `SELECT status,attempt_count,last_error FROM outbox_events WHERE id='evt-dead'`).Scan(&status,&attempts,&lastError); err != nil { t.Fatal(err) }
    if status != "pending" || attempts != 0 || lastError != nil { t.Fatalf("requeued head status=%s attempts=%d error=%v", status, attempts, lastError) }
    if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM outbox_events WHERE id='evt-later'`).Scan(&status); err != nil { t.Fatal(err) }
    if status != "pending" { t.Fatalf("later fact changed to %s", status) }

    claimed, err := svc.ClaimNext(ctx, "recovery-test")
    if err != nil { t.Fatal(err) }
    if claimed == nil || claimed.ID != "evt-dead" { t.Fatalf("claimed=%+v want approved dead-letter head", claimed) }
}

func TestRequeueDeadLetterFailsClosedForWrongScopeOrState(t *testing.T) {
    db := testutil.OpenDatabase(t)
    ctx := context.Background()
    now := time.Now().UTC().Format(time.RFC3339Nano)
    if _, err := db.SQL().ExecContext(ctx, `INSERT INTO outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,payload_json,metadata_json,status,attempt_count,available_at,created_at) VALUES('evt-dead','sales_order','ord-1',1,'sale.partial_returned',1,'sales_order:ord-1','{}','{}','dead_letter',12,?,?)`, now, now); err != nil { t.Fatal(err) }
    svc := New(db)
    if err := svc.RequeueDeadLetter(ctx, "evt-dead", "sales_order:other"); err != ErrRecoveryOrderingMismatch { t.Fatalf("wrong-key error=%v", err) }
    if err := svc.RequeueDeadLetter(ctx, "missing", "sales_order:ord-1"); err != ErrDeadLetterNotFound { t.Fatalf("missing error=%v", err) }
}
