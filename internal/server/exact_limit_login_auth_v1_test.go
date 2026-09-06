package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/localauth"
)

func TestLocalAuthLoginAllowsExact32KiBJSONPayload(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	const limit = 32 << 10
	const prefix = `{"user_id":"`
	const suffix = `","pin":"2468"}`
	padding := limit - len(prefix) - len(suffix)
	if padding <= 0 {
		t.Fatal("invalid exact-limit fixture sizing")
	}
	payload := prefix + strings.Repeat("x", padding) + suffix
	if got := len(payload); got != limit {
		t.Fatalf("expected exact %d-byte login payload, got %d", limit, got)
	}

	s := &Server{localAuth: localauth.New(db, "")}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(payload))
	rec := httptest.NewRecorder()

	s.handleLocalAuthLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected exact-limit valid JSON to reach credential evaluation with 401, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"invalid_local_credentials"`) {
		t.Fatalf("expected invalid_local_credentials, got %s", rec.Body.String())
	}
}