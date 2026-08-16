package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestV1LocalMachineAuthRejectsMissingAndInvalidTokens(t *testing.T) {
	auth, err := LoadOrCreate("device-1", strings.Repeat("a", 32), t.TempDir()+"/token", []string{"http://localhost:3000"})
	if err != nil { t.Fatal(err) }

	called := 0
	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, presented := range []string{"", strings.Repeat("b", 32)} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/diagnostics", nil)
		if presented != "" { req.Header.Set(HeaderLocalToken, presented) }
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized { t.Fatalf("token %q status=%d", presented, res.Code) }
		if !strings.Contains(res.Body.String(), "local_auth_required") { t.Fatalf("missing safe error body: %s", res.Body.String()) }
	}
	if called != 0 { t.Fatalf("protected handler ran %d times", called) }
}

func TestV1LocalMachineAuthAllowsOnlyConfiguredTokenAndOrigin(t *testing.T) {
	token := strings.Repeat("c", 32)
	auth, err := LoadOrCreate("device-1", token, t.TempDir()+"/token", []string{"http://localhost:3000"})
	if err != nil { t.Fatal(err) }

	called := 0
	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))

	allowed := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/diagnostics", nil)
	allowed.Header.Set(HeaderLocalToken, token)
	allowed.Header.Set("Origin", "http://localhost:3000")
	allowedRes := httptest.NewRecorder()
	handler.ServeHTTP(allowedRes, allowed)
	if allowedRes.Code != http.StatusNoContent { t.Fatalf("allowed status=%d", allowedRes.Code) }
	if allowedRes.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" { t.Fatalf("allowed origin not echoed") }

	blocked := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/diagnostics", nil)
	blocked.Header.Set(HeaderLocalToken, token)
	blocked.Header.Set("Origin", "https://attacker.example")
	blockedRes := httptest.NewRecorder()
	handler.ServeHTTP(blockedRes, blocked)
	if blockedRes.Code != http.StatusForbidden { t.Fatalf("blocked origin status=%d", blockedRes.Code) }
	if !strings.Contains(blockedRes.Body.String(), "origin_not_allowed") { t.Fatalf("missing safe origin error") }
	if called != 1 { t.Fatalf("protected handler ran %d times", called) }
}

func TestV1HealthAndReadyRemainPublicToLoopbackSupervisor(t *testing.T) {
	auth, err := LoadOrCreate("device-1", strings.Repeat("d", 32), t.TempDir()+"/token", nil)
	if err != nil { t.Fatal(err) }
	called := 0
	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(http.StatusNoContent) }))
	for _, path := range []string{"/api/v1/health", "/api/v1/ready"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil))
		if res.Code != http.StatusNoContent { t.Fatalf("%s status=%d", path, res.Code) }
	}
	if called != 2 { t.Fatalf("public supervisor calls=%d", called) }
}
