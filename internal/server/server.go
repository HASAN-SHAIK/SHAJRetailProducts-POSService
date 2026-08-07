package server

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
)

type Server struct {
    httpServer *http.Server
    startedAt  time.Time
    cfg        config.Config
}

func New(cfg config.Config) *Server {
    s := &Server{cfg: cfg, startedAt: time.Now().UTC()}

    mux := http.NewServeMux()
    mux.HandleFunc("GET /api/v1/health", s.handleHealth)
    mux.HandleFunc("GET /api/v1/ready", s.handleReady)

    s.httpServer = &http.Server{
        Addr:              cfg.ListenAddress,
        Handler:           requestIDMiddleware(securityHeadersMiddleware(mux)),
        ReadHeaderTimeout: 5 * time.Second,
        ReadTimeout:       15 * time.Second,
        WriteTimeout:      15 * time.Second,
        IdleTimeout:       60 * time.Second,
    }

    return s
}

func (s *Server) Start() error {
    err := s.httpServer.ListenAndServe()
    if errors.Is(err, http.ErrServerClosed) {
        return nil
    }
    return err
}

func (s *Server) Shutdown(ctx context.Context) error {
    return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
    writeJSON(w, http.StatusOK, map[string]any{
        "status":      "ok",
        "service":     "shajretail-pos-service",
        "environment": s.cfg.Environment,
        "started_at":  s.startedAt,
        "uptime_ms":   time.Since(s.startedAt).Milliseconds(),
    })
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
    writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(payload)
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("Cache-Control", "no-store")
        next.ServeHTTP(w, r)
    })
}

func requestIDMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        requestID := r.Header.Get("X-Request-ID")
        if requestID == "" {
            requestID = time.Now().UTC().Format("20060102T150405.000000000")
        }
        w.Header().Set("X-Request-ID", requestID)
        next.ServeHTTP(w, r)
    })
}
