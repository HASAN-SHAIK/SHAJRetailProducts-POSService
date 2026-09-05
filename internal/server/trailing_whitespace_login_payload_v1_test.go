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

func TestLocalAuthLoginAllowsTrailingJSONWhitespace(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }

	s := &Server{localAuth: localauth.New(db, "")}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{\"user_id\":\"missing-cycle-c\",\"pin\":\"2468\"} \n\t  \r\n"))
	rec := httptest.NewRecorder()
	s.handleLocalAuthLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected syntactically valid login JSON with trailing whitespace to reach credential evaluation and return 401, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"invalid_local_credentials"`) {
		t.Fatalf("expected invalid_local_credentials, got %s", rec.Body.String())
	}
}
