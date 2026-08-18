package changefeed

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log/slog"
    "net/http"
    "net/url"
    "strings"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
)

const supportedChangeSchemaVersion = 1

type Puller struct {
    db *database.DB
    inbox *inbox.Service
    client *http.Client
    baseURL string
    deviceID string
    tenantID string
    syncToken string
    interval time.Duration
}

type response struct {
    Cursor string `json:"cursor"`
    HasMore bool `json:"has_more"`
    Changes []inbox.Message `json:"changes"`
}

func New(db *database.DB, inboxService *inbox.Service, baseURL, tenantID, syncToken, deviceID string, timeout, interval time.Duration) *Puller {
    return &Puller{db:db,inbox:inboxService,client:&http.Client{Timeout:timeout},baseURL:strings.TrimRight(baseURL,"/"),deviceID:deviceID,
        tenantID:strings.TrimSpace(tenantID),syncToken:strings.TrimSpace(syncToken),interval:interval}
}

func (p *Puller) Run(ctx context.Context) {
    if p.baseURL=="" { return }
    ticker:=time.NewTicker(p.interval); defer ticker.Stop()
    for {
        if err:=p.pullUntilCaughtUp(ctx); err!=nil && ctx.Err()==nil { slog.Warn("change feed pull failed","error",err) }
        select { case <-ctx.Done(): return; case <-ticker.C: }
    }
}

func (p *Puller) pullUntilCaughtUp(ctx context.Context) error {
    for i:=0;i<10;i++ { more,err:=p.pullOnce(ctx); if err!=nil{return err}; if !more{return nil} }
    return nil
}

func (p *Puller) pullOnce(ctx context.Context) (bool,error) {
    cursor,err:=p.cursor(ctx); if err!=nil{return false,err}
    endpoint:=p.baseURL+"/api/v1/sync/changes?limit=100"
    if cursor!="" { endpoint += "&cursor="+url.QueryEscape(cursor) }
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil); if err!=nil{return false,err}
    req.Header.Set("Accept","application/json")
    req.Header.Set("X-POS-Device-ID",p.deviceID)
    req.Header.Set("X-POS-Tenant-ID",p.tenantID)
    req.Header.Set("X-POS-Sync-Token",p.syncToken)
    resp,err:=p.client.Do(req); if err!=nil{return false,err}; defer resp.Body.Close()
    body,err:=io.ReadAll(io.LimitReader(resp.Body,4<<20)); if err!=nil{return false,err}
    if resp.StatusCode<200 || resp.StatusCode>=300 { return false,fmt.Errorf("change_feed_http_%d",resp.StatusCode) }
    var envelope response
    if err:=json.Unmarshal(body,&envelope); err!=nil{return false,fmt.Errorf("decode change feed: %w",err)}
    for _,change:=range envelope.Changes {
        if change.SchemaVersion > supportedChangeSchemaVersion {
            return false,fmt.Errorf("unsupported_change_schema:%s:v%d",change.Type,change.SchemaVersion)
        }
        if err:=p.inbox.Apply(ctx,change); err!=nil{return false,fmt.Errorf("apply %s: %w",change.ID,err)}
    }
    if envelope.Cursor!="" && envelope.Cursor!=cursor { if err:=p.saveCursor(ctx,envelope.Cursor); err!=nil{return false,err} }
    return envelope.HasMore,nil
}

func (p *Puller) cursor(ctx context.Context) (string,error) {
    var cursor string
    err:=p.db.SQL().QueryRowContext(ctx,`SELECT cursor_value FROM sync_checkpoints WHERE stream_name='central_changes'`).Scan(&cursor)
    if errors.Is(err,sql.ErrNoRows) { return "",nil }
    if err!=nil { return "",err }
    return cursor,nil
}

func (p *Puller) saveCursor(ctx context.Context,cursor string) error {
    now:=time.Now().UTC().Format(time.RFC3339Nano)
    _,err:=p.db.SQL().ExecContext(ctx,`INSERT INTO sync_checkpoints(stream_name,cursor_value,updated_at) VALUES('central_changes',?,?) ON CONFLICT(stream_name) DO UPDATE SET cursor_value=excluded.cursor_value,updated_at=excluded.updated_at`,cursor,now)
    return err
}
