package app

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "log/slog"
    "net/http"
    "strings"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/effectiveconfig"
)

type App struct {
    cfg           config.Config
    logger        *slog.Logger
    server        *http.Server
    db            *database.DB
    device        *device.Service
    configService *effectiveconfig.Service
    runCancel     context.CancelFunc
}

func New(cfg config.Config, logger *slog.Logger) *App {
    mux := http.NewServeMux()
    app := &App{cfg: cfg, logger: logger}

    mux.HandleFunc("GET /api/v1/health", app.health)
    mux.HandleFunc("GET /api/v1/status", app.status)
    mux.HandleFunc("GET /api/v1/config", app.getConfig)
    mux.HandleFunc("POST /api/v1/config/refresh", app.refreshConfig)

    app.server = &http.Server{
        Addr:              cfg.ListenAddress,
        Handler:           requestLogger(logger, corsMiddleware(cfg.AllowedOrigins, mux)),
        ReadHeaderTimeout: 5 * time.Second,
        ReadTimeout:       15 * time.Second,
        WriteTimeout:      15 * time.Second,
        IdleTimeout:       60 * time.Second,
    }
    return app
}

func (a *App) Start() error {
    if err := a.initialize(context.Background()); err != nil { return err }
    runCtx, cancel := context.WithCancel(context.Background())
    a.runCancel = cancel
    if a.configService != nil && a.configService.Enabled() { go a.configService.Run(runCtx) }

    a.logger.Info("starting local POS service", "address", a.cfg.ListenAddress, "environment", a.cfg.Environment)
    err := a.server.ListenAndServe()
    if errors.Is(err, http.ErrServerClosed) { return nil }
    return err
}

func (a *App) initialize(ctx context.Context) error {
    db, err := database.Open(ctx, a.cfg.DatabasePath)
    if err != nil { return err }
    if err := db.Migrate(ctx); err != nil { _ = db.Close(); return err }

    deviceService := device.New(db)
    identity, err := deviceService.EnsureInstallation(ctx)
    if err != nil { _ = db.Close(); return err }

    store := effectiveconfig.NewStore(db)
    client, err := effectiveconfig.NewClient(
        a.cfg.CentralAPIURL,
        a.cfg.CentralTenantID,
        identity.DeviceID,
        a.cfg.CentralSyncToken,
        a.cfg.SyncRequestTimeout,
    )
    if err != nil { _ = db.Close(); return err }

    a.db = db
    a.device = deviceService
    a.configService = effectiveconfig.NewService(store, client, a.logger, time.Minute)
    return nil
}

func (a *App) Shutdown(ctx context.Context) error {
    a.logger.Info("stopping local POS service")
    if a.runCancel != nil { a.runCancel() }
    serverErr := a.server.Shutdown(ctx)
    if a.db != nil {
        if err := a.db.Close(); err != nil && serverErr == nil { return err }
    }
    return serverErr
}

func (a *App) health(w http.ResponseWriter, _ *http.Request) {
    writeJSON(w, http.StatusOK, map[string]any{
        "status": "ok",
        "service": "shajretail-pos-service",
        "environment": a.cfg.Environment,
        "time": time.Now().UTC(),
    })
}

func (a *App) status(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
    defer cancel()

    response := map[string]any{"status": "ready", "database": "healthy"}
    if a.db == nil || a.db.Ping(ctx) != nil {
        response["status"] = "degraded"
        response["database"] = "unavailable"
    }
    if a.device != nil {
        if identity, err := a.device.Get(ctx); err == nil { response["device"] = identity }
    }
    if a.configService != nil {
        state, _ := a.configService.State(ctx)
        response["configuration_sync"] = state
        if snapshot, err := a.configService.Snapshot(ctx); err == nil {
            response["configuration"] = map[string]any{
                "available": true,
                "etag": snapshot.ETag,
                "fetched_at": snapshot.FetchedAt,
                "generated_at": snapshot.GeneratedAt,
            }
        } else if errors.Is(err, sql.ErrNoRows) {
            response["configuration"] = map[string]any{"available": false}
        }
    }
    writeJSON(w, http.StatusOK, response)
}

func (a *App) getConfig(w http.ResponseWriter, r *http.Request) {
    if a.configService == nil { writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "config_unavailable"}); return }
    snapshot, err := a.configService.Snapshot(r.Context())
    if errors.Is(err, sql.ErrNoRows) {
        state, _ := a.configService.State(r.Context())
        writeJSON(w, http.StatusNotFound, map[string]any{"code": "config_snapshot_missing", "sync": state})
        return
    }
    if err != nil { writeJSON(w, http.StatusInternalServerError, map[string]any{"code": "config_snapshot_failed"}); return }
    w.Header().Set("ETag", `"`+snapshot.ETag+`"`)
    writeJSON(w, http.StatusOK, snapshot)
}

func (a *App) refreshConfig(w http.ResponseWriter, r *http.Request) {
    if a.configService == nil || !a.configService.Enabled() {
        writeJSON(w, http.StatusConflict, map[string]any{"code": "central_config_disabled"})
        return
    }
    ctx, cancel := context.WithTimeout(r.Context(), a.cfg.SyncRequestTimeout+2*time.Second)
    defer cancel()
    changed, err := a.configService.Refresh(ctx)
    if err != nil {
        state, _ := a.configService.State(ctx)
        writeJSON(w, http.StatusBadGateway, map[string]any{"code": "config_refresh_failed", "message": err.Error(), "sync": state})
        return
    }
    snapshot, snapErr := a.configService.Snapshot(ctx)
    if snapErr != nil { writeJSON(w, http.StatusInternalServerError, map[string]any{"code": "config_snapshot_failed"}); return }
    writeJSON(w, http.StatusOK, map[string]any{"changed": changed, "snapshot": snapshot})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(payload)
}

func corsMiddleware(allowed []string, next http.Handler) http.Handler {
    allowedSet := map[string]struct{}{}
    for _, origin := range allowed { allowedSet[strings.TrimRight(strings.TrimSpace(origin), "/")] = struct{}{} }
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
        if origin != "" {
            if _, ok := allowedSet[origin]; !ok { writeJSON(w, http.StatusForbidden, map[string]any{"code": "origin_not_allowed"}); return }
            w.Header().Set("Access-Control-Allow-Origin", origin)
            w.Header().Set("Access-Control-Allow-Credentials", "true")
            w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-POS-Local-Token")
            w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
            w.Header().Add("Vary", "Origin")
        }
        if r.Method == http.MethodOptions { w.WriteHeader(http.StatusNoContent); return }
        next.ServeHTTP(w, r)
    })
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        started := time.Now()
        next.ServeHTTP(w, r)
        logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
    })
}
