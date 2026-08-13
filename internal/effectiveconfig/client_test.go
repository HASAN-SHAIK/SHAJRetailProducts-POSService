package effectiveconfig

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"
)

func TestClientSendsMachineCredentialsAndETag(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/v1/sync/config/effective" { t.Fatalf("unexpected path %s", r.URL.Path) }
        if r.Header.Get("X-POS-Tenant-ID") != "tenant-1" { t.Fatal("missing tenant header") }
        if r.Header.Get("X-POS-Device-ID") != "device-1" { t.Fatal("missing device header") }
        if r.Header.Get("X-POS-Sync-Token") != "sync-token" { t.Fatal("missing sync token") }
        if r.Header.Get("If-None-Match") != `"etag-old"` { t.Fatalf("unexpected etag header %q", r.Header.Get("If-None-Match")) }
        _ = json.NewEncoder(w).Encode(Snapshot{
            SchemaVersion: 1,
            ETag: "etag-new",
            Scope: Scope{TenantID: "tenant-1", DeviceID: "device-1"},
            Values: map[string]any{"offline.sales_enabled": true},
            Config: map[string]any{"offline": map[string]any{"sales_enabled": true}},
        })
    }))
    defer server.Close()

    client, err := NewClient(server.URL, "tenant-1", "device-1", "sync-token", time.Second)
    if err != nil { t.Fatal(err) }
    result, err := client.Fetch(context.Background(), "etag-old")
    if err != nil { t.Fatal(err) }
    if !result.Changed || result.Snapshot.ETag != "etag-new" { t.Fatalf("unexpected result: %+v", result) }
}

func TestClientHandlesNotModified(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusNotModified)
    }))
    defer server.Close()

    client, err := NewClient(server.URL, "tenant-1", "device-1", "sync-token", time.Second)
    if err != nil { t.Fatal(err) }
    result, err := client.Fetch(context.Background(), "etag-current")
    if err != nil { t.Fatal(err) }
    if result.Changed { t.Fatal("304 must not be reported as changed") }
}
