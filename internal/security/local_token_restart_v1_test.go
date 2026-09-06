package security

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV1LoadOrCreateReloadsValidPersistedLocalToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "pos.token")
	token := strings.Repeat("r", 32)
	original := []byte(token + "\n")
	if err := os.WriteFile(tokenFile, original, 0o600); err != nil {
		t.Fatalf("write persisted token: %v", err)
	}

	auth, err := LoadOrCreate("device-cycle-c", "", tokenFile, []string{"http://127.0.0.1:5173"})
	if err != nil {
		t.Fatalf("reload valid persisted token: %v", err)
	}

	called := 0
	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/diagnostics", nil)
	req.Header.Set(HeaderLocalToken, token)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("persisted token was not accepted after reload: status=%d called=%d body=%s", res.Code, called, res.Body.String())
	}

	after, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read token file after reload: %v", err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("valid persisted token was unexpectedly modified: got %q want %q", after, original)
	}
}
