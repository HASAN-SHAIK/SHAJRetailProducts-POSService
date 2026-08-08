package server

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestManagerApprovalIsBoundAndSingleUse(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }

	s := &Server{db: db}
	token := "one-time-approval-token"
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(token_hash,cashier_user_id,approver_user_id,permission,reason,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?)`,
		hash[:], "cashier-1", "manager-1", permissionPOSDiscount, "customer retention", now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil { t.Fatal(err) }

	approval, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSDiscount)
	if err != nil { t.Fatalf("first consume: %v", err) }
	if approval.ApproverUserID != "manager-1" || approval.Reason != "customer retention" {
		t.Fatalf("unexpected approval: %+v", approval)
	}
	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSDiscount); err == nil {
		t.Fatal("expected second consume to fail")
	}
}

func TestDiscountedOrderCanUseOneTimeApproval(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate: %v", err) }

	s := &Server{db: db}
	token := "discount-approval-token"
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO pos_manager_approvals(token_hash,cashier_user_id,approver_user_id,permission,created_at,expires_at)
		VALUES(?,?,?,?,?,?)`,
		hash[:], "cashier-1", "manager-1", permissionPOSDiscount, now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil { t.Fatal(err) }

	nextCalled := false
	handler := s.requireOrderWrite(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		approval, ok := approvalFromContext(r.Context())
		if !ok || approval.ApproverUserID != "manager-1" { t.Fatalf("approval missing from context: %+v", approval) }
		w.WriteHeader(http.StatusNoContent)
	})

	user := LocalUserContext{UserID: "cashier-1", Permissions: []string{permissionPOSSale}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"items":[{"discount_minor":250}]}`))
	req.Header.Set("X-POS-Approval-Token", token)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, user))
	res := httptest.NewRecorder()
	handler(res, req)

	if !nextCalled || res.Code != http.StatusNoContent {
		t.Fatalf("approved discount denied: called=%v status=%d body=%s", nextCalled, res.Code, res.Body.String())
	}

	replay := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"items":[{"discount_minor":250}]}`))
	replay.Header.Set("X-POS-Approval-Token", token)
	replay = replay.WithContext(context.WithValue(replay.Context(), authContextKey{}, user))
	replayRes := httptest.NewRecorder()
	handler(replayRes, replay)
	if replayRes.Code != http.StatusForbidden || !strings.Contains(replayRes.Body.String(), "manager_approval_required") {
		t.Fatalf("replayed approval status=%d body=%s", replayRes.Code, replayRes.Body.String())
	}
}
