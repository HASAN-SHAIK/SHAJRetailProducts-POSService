package effectiveconfig

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/url"
    "strings"
    "time"
)

type Client struct {
    httpClient *http.Client
    endpoint   string
    tenantID   string
    deviceID   string
    syncToken  string
}

type FetchResult struct {
    Changed  bool
    Snapshot Snapshot
}

func NewClient(centralURL, tenantID, deviceID, syncToken string, timeout time.Duration) (*Client, error) {
    centralURL = strings.TrimSpace(centralURL)
    if centralURL == "" { return nil, nil }
    parsed, err := url.Parse(centralURL)
    if err != nil || parsed.Scheme == "" || parsed.Host == "" { return nil, errors.New("invalid central API URL") }
    if parsed.Scheme != "https" && !isLoopbackHost(parsed.Hostname()) { return nil, errors.New("central API must use HTTPS outside loopback development") }
    if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(deviceID) == "" || strings.TrimSpace(syncToken) == "" {
        return nil, errors.New("tenant id, device id and sync token are required for config refresh")
    }
    if timeout <= 0 { timeout = 10 * time.Second }
    return &Client{
        httpClient: &http.Client{Timeout: timeout},
        endpoint: strings.TrimRight(centralURL, "/") + "/api/v1/sync/config/effective",
        tenantID: strings.TrimSpace(tenantID),
        deviceID: strings.TrimSpace(deviceID),
        syncToken: strings.TrimSpace(syncToken),
    }, nil
}

func (c *Client) Fetch(ctx context.Context, currentETag string) (FetchResult, error) {
    if c == nil { return FetchResult{}, errors.New("central configuration client is disabled") }
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
    if err != nil { return FetchResult{}, err }
    req.Header.Set("Accept", "application/json")
    req.Header.Set("X-POS-Tenant-ID", c.tenantID)
    req.Header.Set("X-POS-Device-ID", c.deviceID)
    req.Header.Set("X-POS-Sync-Token", c.syncToken)
    if strings.TrimSpace(currentETag) != "" { req.Header.Set("If-None-Match", `"`+strings.TrimSpace(currentETag)+`"`) }

    resp, err := c.httpClient.Do(req)
    if err != nil { return FetchResult{}, fmt.Errorf("central configuration request: %w", err) }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusNotModified { return FetchResult{Changed: false}, nil }
    limited, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
    if err != nil { return FetchResult{}, fmt.Errorf("read central configuration response: %w", err) }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return FetchResult{}, fmt.Errorf("central configuration returned %d: %s", resp.StatusCode, strings.TrimSpace(string(limited)))
    }
    var snapshot Snapshot
    if err := json.Unmarshal(limited, &snapshot); err != nil { return FetchResult{}, fmt.Errorf("decode central configuration: %w", err) }
    if snapshot.SchemaVersion <= 0 || snapshot.ETag == "" { return FetchResult{}, errors.New("central configuration response is incomplete") }
    if snapshot.Scope.DeviceID != "" && snapshot.Scope.DeviceID != c.deviceID { return FetchResult{}, errors.New("central configuration device mismatch") }
    return FetchResult{Changed: true, Snapshot: snapshot}, nil
}

func isLoopbackHost(host string) bool {
    if strings.EqualFold(host, "localhost") { return true }
    ip := net.ParseIP(host)
    return ip != nil && ip.IsLoopback()
}
