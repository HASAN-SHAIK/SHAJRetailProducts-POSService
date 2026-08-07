package server

import (
    "log/slog"
    "net/http"
    "time"
)

type statusRecorder struct {
    http.ResponseWriter
    status int
}

func (w *statusRecorder) WriteHeader(status int) {
    w.status = status
    w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(body []byte) (int, error) {
    if w.status == 0 { w.status = http.StatusOK }
    return w.ResponseWriter.Write(body)
}

// requestMetricsMiddleware deliberately avoids logging URL paths, query
// strings, headers, bodies, customer IDs, order IDs, or payment references.
func requestMetricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        started := time.Now()
        recorder := &statusRecorder{ResponseWriter: w}
        next.ServeHTTP(recorder, r)
        status := recorder.status
        if status == 0 { status = http.StatusOK }
        slog.Info("local API request",
            "method", r.Method,
            "status", status,
            "duration_ms", time.Since(started).Milliseconds(),
        )
    })
}
