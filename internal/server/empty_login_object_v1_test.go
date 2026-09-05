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

func TestLocalAuthLoginRejectsEmptyJSONObject(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	s := &Server{localAuth: localauth.New(db, "")}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	s.handleLocalAuthLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected empty JSON login object to be rejected with 400, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"invalid_auth_payload"`) {
		t.Fatalf("expected invalid_auth_payload, got %s", rec.Body.String())
	}
}
