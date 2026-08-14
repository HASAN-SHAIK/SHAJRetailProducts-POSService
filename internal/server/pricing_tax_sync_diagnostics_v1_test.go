package server

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1PricingTaxSyncStateIsVisibleToSupportDiagnostics(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO effective_config_sync_state(
			singleton_id,last_attempt_at,last_success_at,last_error,last_etag,updated_at
		) VALUES(1,?,?,?,?,?)
		ON CONFLICT(singleton_id) DO UPDATE SET
			last_attempt_at=excluded.last_attempt_at,
			last_success_at=excluded.last_success_at,
			last_error=excluded.last_error,
			last_etag=excluded.last_etag,
			updated_at=excluded.updated_at`,
		"2026-08-15T01:00:00Z",
		"2026-08-15T00:55:00Z",
		"central configuration unavailable",
		"etag-pricing-v1",
		"2026-08-15T01:00:00Z",
	)
	if err != nil {
		t.Fatalf("seed effective config sync state: %v", err)
	}

	s := &Server{db: db}
	state, err := s.loadEffectiveConfigDiagnostics(ctx)
	if err != nil {
		t.Fatalf("load effective config diagnostics: %v", err)
	}
	if state.LastAttemptAt != "2026-08-15T01:00:00Z" {
		t.Fatalf("last attempt missing: %+v", state)
	}
	if state.LastSuccessAt != "2026-08-15T00:55:00Z" {
		t.Fatalf("last success missing: %+v", state)
	}
	if state.LastError != "central configuration unavailable" {
		t.Fatalf("last error missing: %+v", state)
	}
	if state.LastETag != "etag-pricing-v1" {
		t.Fatalf("last etag missing: %+v", state)
	}
}
