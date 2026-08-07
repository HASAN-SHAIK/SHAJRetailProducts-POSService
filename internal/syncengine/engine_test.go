package syncengine

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

func testEvent() outbox.Event {
    return outbox.Event{
        ID: "evt-1", AggregateType: "sales_order", AggregateID: "ord-1", AggregateVersion: 2,
        EventType: "sale.completed", SchemaVersion: 1, OrderingKey: "sales_order:ord-1",
        Payload: json.RawMessage(`{"order":{"id":"ord-1"}}`), Metadata: json.RawMessage(`{"source":"pos_service"}`),
        CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
    }
}

func TestPublishUsesCentralPOSContract(t *testing.T) {
    var got Envelope
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/v1/sync/events" { t.Errorf("path=%s", r.URL.Path) }
        if r.Header.Get("Idempotency-Key") != "evt-1" { t.Errorf("missing idempotency key") }
        if r.Header.Get("X-POS-Device-ID") != "dev-1" { t.Errorf("device header") }
        if r.Header.Get("X-POS-Tenant-ID") != "tenant-1" { t.Errorf("tenant header") }
        if r.Header.Get("X-POS-Sync-Token") != "secret" { t.Errorf("token header") }
        if err := json.NewDecoder(r.Body).Decode(&got); err != nil { t.Errorf("decode: %v", err) }
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    engine, err := New(nil, server.URL, "tenant-1", "secret", "dev-1", time.Second, time.Second)
    if err != nil { t.Fatal(err) }
    if err := engine.publish(context.Background(), testEvent()); err != nil { t.Fatal(err) }
    if got.EventID != "evt-1" || got.EventType != "sale.completed" || got.AggregateID != "ord-1" { t.Fatalf("unexpected envelope: %#v", got) }
}

func TestConflictIsAcceptedAsIdempotentAck(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusConflict) }))
    defer server.Close()
    engine, err := New(nil, server.URL, "tenant-1", "secret", "dev-1", time.Second, time.Second)
    if err != nil { t.Fatal(err) }
    if err := engine.publish(context.Background(), testEvent()); err != nil { t.Fatalf("conflict should ack duplicate: %v", err) }
}

func TestRemoteCentralRequiresHTTPSAndCredentials(t *testing.T) {
    if _, err := New(nil, "http://example.com", "tenant-1", "secret", "dev-1", time.Second, time.Second); err == nil { t.Fatal("expected HTTPS validation error") }
    if _, err := New(nil, "https://example.com", "", "", "dev-1", time.Second, time.Second); err == nil { t.Fatal("expected credential validation error") }
}
