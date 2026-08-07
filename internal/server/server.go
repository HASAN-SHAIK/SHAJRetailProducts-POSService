package server

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
)

type Server struct {
    httpServer *http.Server
    startedAt  time.Time
    cfg        config.Config
    db         *database.DB
    device     *device.Service
}

func New(cfg config.Config, db *database.DB, deviceService *device.Service) *Server {
    s := &Server{cfg: cfg, db: db, device: deviceService, startedAt: time.Now().UTC()}

    mux := http.NewServeMux()
    mux.HandleFunc("GET /api/v1/health", s.handleHealth)
    mux.HandleFunc("GET /api/v1/ready", s.handleReady)
    mux.HandleFunc("GET /api/v1/device", s.handleGetDevice)
    mux.HandleFunc("PUT /api/v1/device/registration", s.handleDeviceRegistration)
    mux.HandleFunc("POST /api/v1/device/heartbeat", s.handleDeviceHeartbeat)

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

func (s *Server) Shutdown(ctx context.Context) error { return s.httpServer.Shutdown(ctx) }

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
    writeJSON(w, http.StatusOK, map[string]any{
        "status": "ok", "service": "shajretail-pos-service", "environment": s.cfg.Environment,
        "started_at": s.startedAt, "uptime_ms": time.Since(s.startedAt).Milliseconds(),
    })
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
    defer cancel()
    if err := s.db.Ping(ctx); err != nil {
        writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": "database_unavailable"})
        return
    }
    if _, err := s.device.Get(ctx); err != nil {
        writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": "device_identity_unavailable"})
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
    identity, err := s.device.Get(r.Context())
    if err != nil {
        writeError(w, http.StatusInternalServerError, "device_identity_unavailable")
        return
    }
    writeJSON(w, http.StatusOK, identity)
}

func (s *Server) handleDeviceRegistration(w http.ResponseWriter, r *http.Request) {
    var input device.Registration
    dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
    dec.DisallowUnknownFields()
    if err := dec.Decode(&input); err != nil {
        writeError(w, http.StatusBadRequest, "invalid_registration_payload")
        return
    }
    identity, err := s.device.ApplyRegistration(r.Context(), input)
    if err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, identity)
}

func (s *Server) handleDeviceHeartbeat(w http.ResponseWriter, r *http.Request) {
    if err := s.device.RecordHeartbeat(r.Context()); err != nil {
        writeError(w, http.StatusInternalServerError, "heartbeat_failed")
        return
    }
    w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string) {
    writeJSON(w, status, map[string]any{"error": code})
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
