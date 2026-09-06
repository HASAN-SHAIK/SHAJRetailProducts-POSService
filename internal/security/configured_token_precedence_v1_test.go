package security

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV1ConfiguredLocalTokenOverridesInvalidPersistedFileWithoutMutation(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "security", "pos.token")
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o750); err != nil {
		t.Fatalf("mkdir token dir: %v", err)
	}
	const stale = "short-stale-token\n"
	if err := os.WriteFile(tokenFile, []byte(stale), 0o600); err != nil {
		t.Fatalf("write stale token: %v", err)
	}

	configured := strings.Repeat("c", 64)
	auth, err := LoadOrCreate("device-cycle-c-configured", configured, tokenFile, nil)
	if err != nil {
		t.Fatalf("configured token should override stale file: %v", err)
	}

	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(raw) != stale {
		t.Fatalf("configured-token startup mutated persisted file: %q", string(raw))
	}

	called := 0
	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))

	allowed := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/diagnostics", nil)
	allowed.Header.Set(HeaderLocalToken, configured)
	allowedRes := httptest.NewRecorder()
	handler.ServeHTTP(allowedRes, allowed)
	if allowedRes.Code != http.StatusNoContent {
		t.Fatalf("configured token status=%d", allowedRes.Code)
	}

	staleReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/diagnostics", nil)
	staleReq.Header.Set(HeaderLocalToken, strings.TrimSpace(stale))
	staleRes := httptest.NewRecorder()
	handler.ServeHTTP(staleRes, staleReq)
	if staleRes.Code != http.StatusUnauthorized || !strings.Contains(staleRes.Body.String(), "local_auth_required") {
		t.Fatalf("stale token unexpectedly accepted: status=%d body=%s", staleRes.Code, staleRes.Body.String())
	}
	if called != 1 {
		t.Fatalf("protected handler called %d times", called)
	}
}
