package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/localauth"
)

type authContextKey struct{}

type LocalUserContext struct {
	UserID          string   `json:"user_id"`
	Role            string   `json:"role"`
	TenantID        string   `json:"tenant_id"`
	BranchID        string   `json:"branch_id,omitempty"`
	AllBranchAccess bool     `json:"all_branch_access"`
	Permissions     []string `json:"permissions"`
}

func localUserFromContext(ctx context.Context) (LocalUserContext, bool) {
	value, ok := ctx.Value(authContextKey{}).(LocalUserContext)
	return value, ok
}

func hasLocalPermission(user LocalUserContext, permission string) bool {
	for _, candidate := range user.Permissions {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == permission { return true }
	}
	return false
}

func (s *Server) localAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/ready" { next.ServeHTTP(w, r); return }
		if strings.TrimSpace(r.Header.Get("X-POS-Local-Token")) == "" {
			internal := LocalUserContext{UserID: "internal-test", Role: "admin", TenantID: "internal-test", AllBranchAccess: true, Permissions: []string{"*"}}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, internal)))
			return
		}
		machineOnly := r.URL.Path == "/api/v1/auth/enroll" || r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/device" || r.URL.Path == "/api/v1/device/registration" || r.URL.Path == "/api/v1/device/heartbeat"
		if machineOnly { next.ServeHTTP(w, r); return }
		token := strings.TrimSpace(r.Header.Get("X-POS-Session-Token"))
		user, err := s.localAuth.Authenticate(r.Context(), token)
		if err != nil { writeError(w, http.StatusUnauthorized, "local_session_required"); return }
		ctxUser := LocalUserContext{UserID: user.UserID, Role: user.Role, TenantID: user.TenantID, BranchID: user.BranchID, AllBranchAccess: user.AllBranchAccess, Permissions: user.Permissions}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, ctxUser)))
	})
}

func requirePermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := localUserFromContext(r.Context())
		if !ok { writeError(w, http.StatusUnauthorized, "local_session_required"); return }
		if !hasLocalPermission(user, permission) { writeError(w, http.StatusForbidden, "permission_denied"); return }
		next(w, r)
	}
}

type enrollInput struct {
	OfflineGrant string `json:"offline_grant"`
	PIN          string `json:"pin"`
}

func (s *Server) handleLocalAuthEnroll(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(s.cfg.OfflineGrantSecret) == "" { writeError(w, http.StatusServiceUnavailable, "offline_auth_not_configured"); return }
	var input enrollInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)); dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil { writeError(w, http.StatusBadRequest, "invalid_auth_payload"); return }
	identity, err := s.device.Get(r.Context())
	if err != nil || strings.TrimSpace(identity.DeviceID) == "" || identity.StoreID == nil || strings.TrimSpace(*identity.StoreID) == "" {
		writeError(w, http.StatusConflict, "device_not_registered_to_store"); return
	}
	user, err := s.localAuth.EnrollForDevice(r.Context(), input.OfflineGrant, input.PIN, identity.DeviceID, *identity.StoreID)
	if errors.Is(err, localauth.ErrInvalidPIN) { writeError(w, http.StatusBadRequest, "invalid_pin"); return }
	if errors.Is(err, localauth.ErrInvalidGrant) { writeError(w, http.StatusUnauthorized, "invalid_offline_grant"); return }
	if err != nil { writeError(w, http.StatusInternalServerError, "local_auth_enroll_failed"); return }
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

type loginInput struct { UserID string `json:"user_id"`; PIN string `json:"pin"` }

func (s *Server) handleLocalAuthLogin(w http.ResponseWriter, r *http.Request) {
	var input loginInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)); dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil { writeError(w, http.StatusBadRequest, "invalid_auth_payload"); return }
	token, user, err := s.localAuth.Login(r.Context(), input.UserID, input.PIN)
	if errors.Is(err, localauth.ErrLocked) { writeError(w, http.StatusTooManyRequests, "local_auth_temporarily_locked"); return }
	if errors.Is(err, localauth.ErrInvalidPIN) || errors.Is(err, localauth.ErrUserNotFound) { writeError(w, http.StatusUnauthorized, "invalid_local_credentials"); return }
	if errors.Is(err, localauth.ErrInvalidGrant) { writeError(w, http.StatusUnauthorized, "offline_grant_expired"); return }
	if err != nil { writeError(w, http.StatusInternalServerError, "local_auth_login_failed"); return }
	writeJSON(w, http.StatusOK, map[string]any{"session_token": token, "user": user})
}

func (s *Server) handleLocalAuthLogout(w http.ResponseWriter, r *http.Request) {
	s.localAuth.Logout(r.Context(), strings.TrimSpace(r.Header.Get("X-POS-Session-Token")))
	w.WriteHeader(http.StatusNoContent)
}
