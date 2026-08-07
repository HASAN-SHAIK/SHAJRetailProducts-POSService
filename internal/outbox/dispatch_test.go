package outbox

import (
    "context"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func seedEvent(t *testing.T, service *Service, id string) {
    t.Helper()
    now := time.Now().UTC().Format(time.RFC3339Nano)
    _, err := service.db.SQL().Exec(`INSERT INTO outbox_events(
        id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
        payload_json,metadata_json,status,attempt_count,available_at,created_at)
        VALUES(?,?,?,?,?,?,?,?,?,'pending',0,?,?)`,
        id,"sales_order","ord-1",1,"sale.completed",1,"sales_order:ord-1","{}","{}",now,now)
    if err != nil { t.Fatalf("seed event: %v", err) }
}

func TestClaimAndPublishIsOwnershipSafe(t *testing.T) {
    db := testutil.OpenDatabase(t)
    service := New(db)
    seedEvent(t, service, "evt-1")
    ctx := context.Background()

    event, err := service.ClaimNext(ctx, "worker-a")
    if err != nil { t.Fatalf("claim: %v", err) }
    if event == nil || event.ID != "evt-1" { t.Fatalf("unexpected event: %#v", event) }
    if err := service.MarkPublished(ctx, event.ID, "worker-b"); err == nil { t.Fatal("expected ownership failure") }
    if err := service.MarkPublished(ctx, event.ID, "worker-a"); err != nil { t.Fatalf("publish: %v", err) }

    var status string
    if err := db.SQL().QueryRow(`SELECT status FROM outbox_events WHERE id='evt-1'`).Scan(&status); err != nil { t.Fatal(err) }
    if status != "published" { t.Fatalf("status=%s", status) }
}

func TestFailedEventIsRetriedLater(t *testing.T) {
    db := testutil.OpenDatabase(t)
    service := New(db)
    seedEvent(t, service, "evt-2")
    ctx := context.Background()

    event, err := service.ClaimNext(ctx, "worker-a")
    if err != nil || event == nil { t.Fatalf("claim: event=%v err=%v", event, err) }
    if err := service.MarkFailed(ctx, event.ID, "worker-a", "central unavailable"); err != nil { t.Fatal(err) }

    var status string
    var attempts int
    var available string
    if err := db.SQL().QueryRow(`SELECT status,attempt_count,available_at FROM outbox_events WHERE id='evt-2'`).Scan(&status,&attempts,&available); err != nil { t.Fatal(err) }
    if status != "failed" || attempts != 1 { t.Fatalf("status=%s attempts=%d", status, attempts) }
    when, err := time.Parse(time.RFC3339Nano, available); if err != nil { t.Fatal(err) }
    if !when.After(time.Now().UTC()) { t.Fatalf("retry not delayed: %s", available) }
}
