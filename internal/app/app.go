package app

import (
    "context"
    "encoding/json"
    "log/slog"
    "net/http"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
)

type App struct {
    cfg    config.Config
    logger *slog.Logger
    server *http.Server
}

func New(cfg config.Config, logger *slog.Logger) *App {
    mux := http.NewServeMux()
    app := &App{cfg: cfg, logger: logger}

    mux.HandleFunc("GET /api/v1/health", app.health)

    app.server = &http.Server{
        Addr:              cfg.ListenAddress,
        Handler:           requestLogger(logger, mux),
        ReadHeaderTimeout: 5 * time.Second,
        ReadTimeout:       15 * time.Second,
        WriteTimeout:      15 * time.Second,
        IdleTimeout:       60 * time.Second,
    }

    return app
}

func (a *App) Start() error {
    a.logger.Info("starting local POS service", "address", a.cfg.ListenAddress, "environment", a.cfg.Environment)
    err := a.server.ListenAndServe()
    if err == http.ErrServerClosed {
        return nil
    }
    return err
}

func (a *App) Shutdown(ctx context.Context) error {
    a.logger.Info("stopping local POS service")
    return a.server.Shutdown(ctx)
}

func (a *App) health(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]any{
        "status":  "ok",
        "service": "shajretail-pos-service",
        "time":    time.Now().UTC(),
    })
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        started := time.Now()
        next.ServeHTTP(w, r)
        logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
    })
}
