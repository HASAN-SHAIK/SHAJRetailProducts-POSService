package app

import (
    "context"
    "encoding/json"
    "io"
    "log/slog"
    "net"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "sync/atomic"
    "testing"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/effectiveconfig"
)

func TestV1LiveEffectiveConfigRefreshPersistsCentralSnapshot(t *testing.T) {
    var armed atomic.Bool
    var centralCalls atomic.Int32
    central := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        centralCalls.Add(1)
        if r.URL.Path != "/api/v1/sync/config/effective" { t.Fatalf("unexpected Central path %s", r.URL.Path) }
        if r.Header.Get("X-POS-Tenant-ID") != "tenant-runtime" { t.Fatalf("unexpected tenant header %q", r.Header.Get("X-POS-Tenant-ID")) }
        if r.Header.Get("X-POS-Sync-Token") != "sync-runtime" { t.Fatalf("unexpected sync token") }
        deviceID := r.Header.Get("X-POS-Device-ID")
        if deviceID == "" { t.Fatal("missing device identity") }
        if !armed.Load() { w.WriteHeader(http.StatusNotModified); return }
        _ = json.NewEncoder(w).Encode(effectiveconfig.Snapshot{
            SchemaVersion: 1,
            GeneratedAt: "2026-08-30T07:00:00Z",
            ETag: "runtime-etag-1",
            Scope: effectiveconfig.Scope{TenantID: "tenant-runtime", DeviceID: deviceID},
            Values: map[string]any{"offline.sales_enabled": true},
            Config: map[string]any{"offline": map[string]any{"sales_enabled": true}},
        })
    }))
    defer central.Close()

    probe, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { t.Fatal(err) }
    addr := probe.Addr().String()
    _ = probe.Close()

    dbPath := filepath.Join(t.TempDir(), "pos.db")
    a := New(config.Config{
        Environment: "test", ListenAddress: addr, DatabasePath: dbPath,
        CentralAPIURL: central.URL, CentralTenantID: "tenant-runtime", CentralSyncToken: "sync-runtime",
        SyncRequestTimeout: time.Second, AllowedOrigins: []string{"http://127.0.0.1:5173"},
    }, slog.New(slog.NewTextHandler(io.Discard, nil)))

    runErr := make(chan error, 1)
    go func() { runErr <- a.Start() }()
    client := &http.Client{Timeout: time.Second}
    base := "http://" + addr
    deadline := time.Now().Add(3 * time.Second)
    for {
        resp, err := client.Get(base + "/api/v1/health")
        if err == nil { _ = resp.Body.Close(); if resp.StatusCode == http.StatusOK { break } }
        if time.Now().After(deadline) { t.Fatalf("POSService did not become healthy: %v", err) }
        time.Sleep(25 * time.Millisecond)
    }
    defer func() {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second); defer cancel()
        if err := a.Shutdown(ctx); err != nil { t.Errorf("shutdown: %v", err) }
        if err := <-runErr; err != nil { t.Errorf("server exit: %v", err) }
    }()

    armed.Store(true)
    req, err := http.NewRequest(http.MethodPost, base+"/api/v1/config/refresh", nil)
    if err != nil { t.Fatal(err) }
    resp, err := client.Do(req)
    if err != nil { t.Fatal(err) }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK { body, _ := io.ReadAll(resp.Body); t.Fatalf("refresh status=%d body=%s", resp.StatusCode, body) }

    getResp, err := client.Get(base + "/api/v1/config")
    if err != nil { t.Fatal(err) }
    defer getResp.Body.Close()
    if getResp.StatusCode != http.StatusOK { t.Fatalf("config status=%d", getResp.StatusCode) }
    if got := getResp.Header.Get("ETag"); got != `"runtime-etag-1"` { t.Fatalf("ETag=%q", got) }
    var snap effectiveconfig.Snapshot
    if err := json.NewDecoder(getResp.Body).Decode(&snap); err != nil { t.Fatal(err) }
    if snap.ETag != "runtime-etag-1" || snap.Values["offline.sales_enabled"] != true { t.Fatalf("unexpected snapshot: %+v", snap) }

    var etag, tenantID, payload string
    if err := a.db.SQL().QueryRow(`SELECT etag, tenant_id, payload_json FROM effective_config_snapshot WHERE singleton_id=1`).Scan(&etag, &tenantID, &payload); err != nil { t.Fatal(err) }
    if etag != "runtime-etag-1" || tenantID != "tenant-runtime" { t.Fatalf("persisted etag=%q tenant=%q", etag, tenantID) }
    if centralCalls.Load() < 2 { t.Fatalf("expected startup plus manual Central fetch, got %d", centralCalls.Load()) }
}
