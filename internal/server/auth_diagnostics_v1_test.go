package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1AuthDiagnosticsExposeOperationalStateWithoutCredentialMaterial(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC()
	future := now.Add(2 * time.Hour).Format(time.RFC3339Nano)
	past := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	locked := now.Add(5 * time.Minute).Format(time.RFC3339Nano)

	seedUser := func(id, grantExpiry string, lockedUntil any) {
		_, err := db.SQL().ExecContext(ctx, `
			INSERT INTO local_users(
				user_id,tenant_id,role,branch_id,all_branch_access,permissions_json,
				pin_salt,pin_hash,pin_iterations,failed_attempts,locked_until,
				grant_id,grant_expires_at,enabled,updated_at
			) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id,"tenant-a","cashier","store-a",0,"[\"pos:sale\"]",
			[]byte("salt"),[]byte("derived-pin-hash"),120000,0,lockedUntil,
			"grant-"+id,grantExpiry,1,now.Format(time.RFC3339Nano),
		)
		if err != nil { t.Fatalf("seed local user %s: %v", id, err) }
	}
	seedUser("active-user", future, nil)
	seedUser("locked-user", future, locked)
	seedUser("expired-user", past, nil)

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO local_auth_sessions(token_hash,user_id,created_at,expires_at,last_seen_at)
		VALUES(?,?,?,?,?)`, []byte("hashed-session-token"), "active-user", now.Format(time.RFC3339Nano), future, now.Format(time.RFC3339Nano))
	if err != nil { t.Fatalf("seed local auth session: %v", err) }

	s := &Server{db: db}
	state, err := s.loadLocalAuthDiagnostics(ctx)
	if err != nil { t.Fatalf("load local auth diagnostics: %v", err) }
	if state.EnrolledUsers != 3 { t.Fatalf("enrolled users=%d", state.EnrolledUsers) }
	if state.ActiveSessions != 1 { t.Fatalf("active sessions=%d", state.ActiveSessions) }
	if state.LockedUsers != 1 { t.Fatalf("locked users=%d", state.LockedUsers) }
	if state.ExpiredGrants != 1 { t.Fatalf("expired grants=%d", state.ExpiredGrants) }
	if state.NextGrantExpiry == "" { t.Fatal("next grant expiry missing") }

	raw, err := json.Marshal(state)
	if err != nil { t.Fatal(err) }
	text := string(raw)
	for _, forbidden := range []string{"pin", "grant_id", "token", "hash", "permissions", "user_id"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("credential-sensitive field %q leaked in diagnostics: %s", forbidden, text)
		}
	}
}
