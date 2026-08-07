package syncengine

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log/slog"
    "net/http"
    "net/url"
    "strings"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
)

type Engine struct {
    outbox      *outbox.Service
    client      *http.Client
    endpoint    string
    deviceID    string
    tenantID    string
    syncToken   string
    owner       string
    poll        time.Duration
}

type Envelope struct {
    EventID          string          `json:"event_id"`
    EventType        string          `json:"event_type"`
    SchemaVersion    int             `json:"schema_version"`
    AggregateType    string          `json:"aggregate_type"`
    AggregateID      string          `json:"aggregate_id"`
    AggregateVersion int             `json:"aggregate_version"`
    OrderingKey      string          `json:"ordering_key"`
    Payload          json.RawMessage `json:"payload"`
    Metadata         json.RawMessage `json:"metadata"`
    CreatedAt        string          `json:"created_at"`
}

func New(service *outbox.Service, centralURL, tenantID, syncToken, deviceID string, timeout, poll time.Duration) (*Engine, error) {
    centralURL = strings.TrimSpace(centralURL)
    if centralURL == "" { return nil, nil }
    parsed, err := url.Parse(centralURL)
    if err != nil || parsed.Scheme == "" || parsed.Host == "" { return nil, fmt.Errorf("invalid POS_CENTRAL_API_URL") }
    if parsed.Scheme != "https" && !isLoopbackHost(parsed.Hostname()) { return nil, errors.New("central API must use HTTPS outside loopback development") }
    if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(syncToken) == "" { return nil, errors.New("central tenant id and sync token are required") }
    if timeout <= 0 { timeout = 10 * time.Second }
    if poll <= 0 { poll = 2 * time.Second }
    endpoint := strings.TrimRight(centralURL, "/") + "/api/v1/sync/events"
    return &Engine{outbox: service, client: &http.Client{Timeout: timeout}, endpoint: endpoint, deviceID: deviceID,
        tenantID: strings.TrimSpace(tenantID), syncToken: strings.TrimSpace(syncToken), owner: "sync:" + deviceID, poll: poll}, nil
}

func (e *Engine) Run(ctx context.Context) {
    if e == nil { return }
    if err := e.outbox.RecoverStaleClaims(ctx, 2*time.Minute); err != nil { slog.Warn("recover stale outbox claims", "error", err) }
    timer := time.NewTimer(0); defer timer.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-timer.C:
            worked := e.dispatchOne(ctx)
            delay := e.poll; if worked { delay = 25 * time.Millisecond }
            timer.Reset(delay)
        }
    }
}

func (e *Engine) dispatchOne(ctx context.Context) bool {
    event, err := e.outbox.ClaimNext(ctx, e.owner)
    if err != nil { slog.Warn("claim sync event", "error", err); return false }
    if event == nil { return false }
    if err := e.publish(ctx, *event); err != nil {
        slog.Warn("publish sync event", "event_id", event.ID, "event_type", event.EventType, "error", err)
        if markErr := e.outbox.MarkFailed(ctx, event.ID, e.owner, err.Error()); markErr != nil { slog.Error("record sync failure", "event_id", event.ID, "error", markErr) }
        return true
    }
    if err := e.outbox.MarkPublished(ctx, event.ID, e.owner); err != nil { slog.Error("acknowledge published event locally", "event_id", event.ID, "error", err); return true }
    slog.Info("sync event published", "event_id", event.ID, "event_type", event.EventType)
    return true
}

func (e *Engine) publish(ctx context.Context, event outbox.Event) error {
    body, err := json.Marshal(Envelope{EventID: event.ID, EventType: event.EventType, SchemaVersion: event.SchemaVersion,
        AggregateType: event.AggregateType, AggregateID: event.AggregateID, AggregateVersion: event.AggregateVersion,
        OrderingKey: event.OrderingKey, Payload: event.Payload, Metadata: event.Metadata, CreatedAt: event.CreatedAt})
    if err != nil { return err }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body)); if err != nil { return err }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "application/json")
    req.Header.Set("Idempotency-Key", event.ID)
    req.Header.Set("X-POS-Device-ID", e.deviceID)
    req.Header.Set("X-POS-Tenant-ID", e.tenantID)
    req.Header.Set("X-POS-Sync-Token", e.syncToken)
    req.Header.Set("X-Ordering-Key", event.OrderingKey)

    resp, err := e.client.Do(req); if err != nil { return fmt.Errorf("central sync request: %w", err) }
    defer resp.Body.Close()
    limited, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
    if resp.StatusCode >= 200 && resp.StatusCode < 300 { return nil }
    if resp.StatusCode == http.StatusConflict { return nil }
    return fmt.Errorf("central sync returned %d: %s", resp.StatusCode, strings.TrimSpace(string(limited)))
}

func isLoopbackHost(host string) bool { switch strings.ToLower(host) { case "localhost", "127.0.0.1", "::1": return true; default: return false } }
