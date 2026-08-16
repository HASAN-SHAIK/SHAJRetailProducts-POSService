package server

import (
	"context"
	"database/sql"
	"time"
)

type localAuthDiagnostics struct {
	EnrolledUsers   int    `json:"enrolled_users"`
	ActiveSessions  int    `json:"active_sessions"`
	LockedUsers     int    `json:"locked_users"`
	ExpiredGrants   int    `json:"expired_grants"`
	NextGrantExpiry string `json:"next_grant_expiry,omitempty"`
}

func (s *Server) loadLocalAuthDiagnostics(ctx context.Context) (localAuthDiagnostics, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var state localAuthDiagnostics
	var nextExpiry sql.NullString
	err := s.db.SQL().QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM local_users WHERE enabled = 1),
			(SELECT COUNT(*)
			 FROM local_auth_sessions sessions
			 JOIN local_users users ON users.user_id = sessions.user_id
			 WHERE users.enabled = 1
			   AND sessions.expires_at > ?
			   AND users.grant_expires_at > ?),
			(SELECT COUNT(*)
			 FROM local_users
			 WHERE enabled = 1
			   AND locked_until IS NOT NULL
			   AND locked_until > ?),
			(SELECT COUNT(*)
			 FROM local_users
			 WHERE enabled = 1
			   AND grant_expires_at <= ?),
			(SELECT MIN(grant_expires_at)
			 FROM local_users
			 WHERE enabled = 1
			   AND grant_expires_at > ?)`,
		now, now, now, now, now,
	).Scan(
		&state.EnrolledUsers,
		&state.ActiveSessions,
		&state.LockedUsers,
		&state.ExpiredGrants,
		&nextExpiry,
	)
	if err != nil {
		return localAuthDiagnostics{}, err
	}
	state.NextGrantExpiry = nullableString(nextExpiry)
	return state, nil
}
