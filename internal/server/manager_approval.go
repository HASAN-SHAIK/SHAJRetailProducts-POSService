package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const approvalTTL = 2 * time.Minute

type approvalContextKey struct{}

type managerApproval struct {
	ApproverUserID string
	Permission     string
	Reason         string
}

type managerApprovalInput struct {
	ManagerUserID string `json:"manager_user_id"`
	PIN           string `json:"pin"`
	Permission    string `json:"permission"`
	Reason        string `json:"reason"`
}

func isApprovablePermission(permission string) bool {
	switch permission {
	case permissionPOSDiscount, permissionPOSVoid, permissionPOSRefund:
		return true
	default:
		return false
	}
}

func approvalRequiresReason(permission string) bool {
	return permission == permissionPOSVoid || permission == permissionPOSRefund
}

func (s *Server) handleManagerApproval(w http.ResponseWriter, r *http.Request) {
	cashier, ok := localUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "local_session_required")
		return
	}

	var input managerApprovalInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_approval_payload")
		return
	}
	input.ManagerUserID = strings.TrimSpace(input.ManagerUserID)
	input.Permission = strings.TrimSpace(input.Permission)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ManagerUserID == "" || input.PIN == "" || !isApprovablePermission(input.Permission) || (approvalRequiresReason(input.Permission) && input.Reason == "") {
		writeError(w, http.StatusBadRequest, "invalid_approval_payload")
		return
	}
	if input.ManagerUserID == cashier.UserID {
		writeError(w, http.StatusForbidden, "self_approval_not_allowed")
		return
	}

	verificationToken, manager, err := s.localAuth.Login(r.Context(), input.ManagerUserID, input.PIN)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "manager_credentials_invalid")
		return
	}
	s.localAuth.Logout(r.Context(), verificationToken)

	managerCtx := LocalUserContext{UserID: manager.UserID, Role: manager.Role, TenantID: manager.TenantID, BranchID: manager.BranchID, AllBranchAccess: manager.AllBranchAccess, Permissions: manager.Permissions}
	if !hasLocalPermission(managerCtx, permissionPOSApprove) || !hasLocalPermission(managerCtx, input.Permission) {
		writeError(w, http.StatusForbidden, "manager_permission_denied")
		return
	}
	if manager.TenantID != cashier.TenantID {
		writeError(w, http.StatusForbidden, "manager_scope_mismatch")
		return
	}
	if !manager.AllBranchAccess && cashier.BranchID != "" && manager.BranchID != cashier.BranchID {
		writeError(w, http.StatusForbidden, "manager_scope_mismatch")
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeError(w, http.StatusInternalServerError, "approval_issue_failed")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	expires := now.Add(approvalTTL)
	_, err = s.db.SQL().ExecContext(r.Context(), `
		INSERT INTO pos_manager_approvals(token_hash,cashier_user_id,approver_user_id,permission,reason,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?)`,
		hash[:], cashier.UserID, manager.UserID, input.Permission, nullableApprovalString(input.Reason), now.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "approval_issue_failed")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"approval_token": token,
		"approver_user_id": manager.UserID,
		"permission": input.Permission,
		"expires_at": expires,
	})
}

func (s *Server) consumeManagerApproval(ctx context.Context, rawToken, cashierUserID, permission string) (managerApproval, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return managerApproval{}, errors.New("approval token missing")
	}
	hash := sha256.Sum256([]byte(rawToken))
	now := time.Now().UTC()

	tx, err := s.db.SQL().BeginTx(ctx, nil)
	if err != nil { return managerApproval{}, err }
	defer tx.Rollback()

	var approval managerApproval
	var reason sql.NullString
	var expiresAt string
	err = tx.QueryRowContext(ctx, `
		SELECT approver_user_id,permission,reason,expires_at
		FROM pos_manager_approvals
		WHERE token_hash=? AND cashier_user_id=? AND permission=? AND consumed_at IS NULL`,
		hash[:], cashierUserID, permission).Scan(&approval.ApproverUserID, &approval.Permission, &reason, &expiresAt)
	if err != nil { return managerApproval{}, err }
	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil || !now.Before(expires) { return managerApproval{}, errors.New("approval expired") }
	if reason.Valid { approval.Reason = reason.String }

	result, err := tx.ExecContext(ctx, `
		UPDATE pos_manager_approvals SET consumed_at=?
		WHERE token_hash=? AND consumed_at IS NULL`, now.Format(time.RFC3339Nano), hash[:])
	if err != nil { return managerApproval{}, err }
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 { return managerApproval{}, errors.New("approval already consumed") }
	if err := tx.Commit(); err != nil { return managerApproval{}, err }
	return approval, nil
}

func approvalFromContext(ctx context.Context) (managerApproval, bool) {
	value, ok := ctx.Value(approvalContextKey{}).(managerApproval)
	return value, ok
}

func (s *Server) requireOrderWrite(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := localUserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "local_session_required")
			return
		}
		if !hasAnyLocalPermission(user, permissionPOSSale, "orders:write") {
			writeError(w, http.StatusForbidden, "permission_denied")
			return
		}

		requiresDiscount, err := orderPayloadHasDiscount(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_order_payload")
			return
		}
		if !requiresDiscount || hasLocalPermission(user, permissionPOSDiscount) {
			next(w, r)
			return
		}

		approval, err := s.consumeManagerApproval(r.Context(), r.Header.Get("X-POS-Approval-Token"), user.UserID, permissionPOSDiscount)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "manager_approval_required",
				"required_permission": permissionPOSDiscount,
			})
			return
		}
		ctx := context.WithValue(r.Context(), approvalContextKey{}, approval)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) recordOrderApproval(ctx context.Context, orderID string) error {
	approval, ok := approvalFromContext(ctx)
	if !ok { return nil }
	_, err := s.db.SQL().ExecContext(ctx,
		`UPDATE sales_orders SET approved_by_user_id=?, approval_reason=? WHERE id=?`,
		approval.ApproverUserID, nullableApprovalString(approval.Reason), orderID)
	return err
}

func nullableApprovalString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" { return nil }
	return value
}
