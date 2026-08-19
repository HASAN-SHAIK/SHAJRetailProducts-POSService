package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const maxRequestIDLength = 128

type requestIDContextKey struct{}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func requestCorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !isSafeRequestID(requestID) {
			requestID = newRequestID()
		}

		// Normalize the request header as well so the existing inner request-ID
		// middleware and any downstream code see only the accepted identifier.
		r.Header.Set("X-Request-ID", requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID))
		w.Header().Set("X-Request-ID", requestID)

		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		slog.Info("local API request",
			"request_id", requestID,
			"method", r.Method,
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func isSafeRequestID(value string) bool {
	if value == "" || len(value) > maxRequestIDLength {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '-', ch == '_', ch == '.', ch == ':':
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	var random [16]byte
	if _, err := rand.Read(random[:]); err == nil {
		return "pos-" + hex.EncodeToString(random[:])
	}
	// crypto/rand failure is exceptional; keep the fallback opaque, bounded,
	// and safe for response headers/log fields rather than trusting caller data.
	return "pos-" + time.Now().UTC().Format("20060102T150405000000000")
}
