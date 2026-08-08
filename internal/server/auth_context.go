package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

type authContextKey struct{}

type LocalUserContext struct {
	UserID      string   `json:"user_id"`
	Role        string   `json:"role"`
	TenantID    string   `json:"tenant_id"`
	BranchID    string   `json:"branch_id"`
	Permissions []string `json:"permissions"`
}

func localUserFromContext(ctx context.Context) (LocalUserContext, bool) {
	value, ok := ctx.Value(authContextKey{}).(LocalUserContext)
	return value, ok
}

func hasLocalPermission(user LocalUserContext, permission string) bool {
	for _, candidate := range user.Permissions {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == permission {
			return true
		}
	}
	return false
}

func (s *Server) localAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/ready" {
			next.ServeHTTP(w, r)
			return
		}

		expected := strings.TrimSpace(s.cfg.LocalAPIToken)
		provided := strings.TrimSpace(r.Header.Get("X-POS-Local-Token"))
		if expected == "" || provided == "" || len(expected) != len(provided) || subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 {
			writeError(w, http.StatusUnauthorized, "local_api_unauthorized")
			return
		}

		permissions := []string{}
		if raw := strings.TrimSpace(r.Header.Get("X-SHAJ-Permissions")); raw != "" {
			if err := json.Unmarshal([]byte(raw), &permissions); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_user_permissions")
				return
			}
		}
		user := LocalUserContext{
			UserID:      strings.TrimSpace(r.Header.Get("X-SHAJ-User-ID")),
			Role:        strings.ToLower(strings.TrimSpace(r.Header.Get("X-SHAJ-User-Role"))),
			TenantID:    strings.TrimSpace(r.Header.Get("X-SHAJ-Tenant-ID")),
			BranchID:    strings.TrimSpace(r.Header.Get("X-SHAJ-Branch-ID")),
			Permissions: permissions,
		}
		if user.UserID == "" || user.TenantID == "" || user.Role == "" {
			writeError(w, http.StatusUnauthorized, "user_context_required")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, user)))
	})
}

func requirePermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := localUserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "user_context_required")
			return
		}
		if !hasLocalPermission(user, permission) {
			writeError(w, http.StatusForbidden, "permission_denied")
			return
		}
		next(w, r)
	}
}
