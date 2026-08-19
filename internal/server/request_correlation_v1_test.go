package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestV1POSRequestCorrelationPreservesSafeCallerIDAcrossResponseContextAndLog(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	const requestID = "cashier-req_123:retry.1"
	handler := requestCorrelationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := requestIDFromContext(r.Context()); got != requestID {
			t.Fatalf("expected request ID in context %q, got %q", requestID, got)
		}
		if got := r.Header.Get("X-Request-ID"); got != requestID {
			t.Fatalf("expected normalized downstream request header %q, got %q", requestID, got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders?secret=do-not-log", nil)
	req.Header.Set("X-Request-ID", requestID)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if got := recorder.Header().Get("X-Request-ID"); got != requestID {
		t.Fatalf("expected response request ID %q, got %q", requestID, got)
	}
	logLine := logs.String()
	for _, expected := range []string{`"request_id":"` + requestID + `"`, `"method":"POST"`, `"status":204`} {
		if !strings.Contains(logLine, expected) {
			t.Fatalf("expected structured log to contain %s: %s", expected, logLine)
		}
	}
	if strings.Contains(logLine, "secret=do-not-log") {
		t.Fatalf("request correlation log leaked query string: %s", logLine)
	}
}

func TestV1POSRequestCorrelationRegeneratesUnsafeOrOversizedCallerIDs(t *testing.T) {
	cases := []string{
		"unsafe request id with spaces",
		strings.Repeat("a", maxRequestIDLength+1),
	}
	for _, supplied := range cases {
		t.Run(supplied[:min(len(supplied), 16)], func(t *testing.T) {
			var downstream string
			handler := requestCorrelationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downstream = requestIDFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			req.Header.Set("X-Request-ID", supplied)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			generated := recorder.Header().Get("X-Request-ID")
			if generated == "" || generated == supplied || !isSafeRequestID(generated) {
				t.Fatalf("expected safe regenerated request ID, supplied=%q generated=%q", supplied, generated)
			}
			if downstream != generated {
				t.Fatalf("response/context request IDs diverged: response=%q context=%q", generated, downstream)
			}
		})
	}
}

func TestV1POSRequestCorrelationGeneratesIDWhenMissing(t *testing.T) {
	var downstream string
	handler := requestCorrelationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstream = requestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	generated := recorder.Header().Get("X-Request-ID")
	if generated == "" || !strings.HasPrefix(generated, "pos-") || !isSafeRequestID(generated) {
		t.Fatalf("expected generated safe POS request ID, got %q", generated)
	}
	if downstream != generated {
		t.Fatalf("response/context request IDs diverged: response=%q context=%q", generated, downstream)
	}
}

func TestRequestIDFromContextDefaultsEmpty(t *testing.T) {
	if got := requestIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty request ID outside middleware, got %q", got)
	}
}
