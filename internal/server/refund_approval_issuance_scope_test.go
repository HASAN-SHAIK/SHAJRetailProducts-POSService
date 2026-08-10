package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/localauth"
)

func TestRefundManagerApprovalAPIStoresExactCentralAuthorizedScope(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "pos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	privateKey, publicPEM := refundApprovalTestKeys(t)
	localAuth := localauth.New(db, publicPEM)
	grant := refundApprovalTestGrant(t, privateKey, map[string]any{
		"type":              "pos_offline_grant",
		"user_id":           "manager-refund-scope",
		"tenant_id":         "tenant-refund-scope",
		"role":              "manager",
		"branch_id":         "branch-refund-scope",
		"all_branch_access": false,
		"permissions":       []string{permissionPOSApprove, permissionPOSRefund},
		"grant_id":          "grant-refund-scope",
		"iss":               "shajtech-central",
		"aud":               "shajtech-pos-edge",
		"exp":               time.Now().Add(time.Hour).Unix(),
	})
	if _, err := localAuth.Enroll(ctx, grant, "2468"); err != nil {
		t.Fatalf("enroll Central-authorized manager: %v", err)
	}

	s := &Server{db: db, localAuth: localAuth}
	cashier := LocalUserContext{
		UserID:      "cashier-refund-scope",
		TenantID:    "tenant-refund-scope",
		BranchID:    "branch-refund-scope",
		Permissions: []string{permissionPOSSale},
	}
	body := `{"manager_user_id":"manager-refund-scope","pin":"2468","permission":"pos:refund","reason":"customer returned one line","order_id":"order-refund-scope","action_scope":"refund_partial"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/approvals", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, cashier))
	res := httptest.NewRecorder()

	s.handleManagerApproval(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("approval issuance status=%d body=%s", res.Code, res.Body.String())
	}

	var issued struct {
		ApprovalToken  string `json:"approval_token"`
		ApproverUserID string `json:"approver_user_id"`
		Permission     string `json:"permission"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode approval response: %v", err)
	}
	if issued.ApprovalToken == "" || issued.ApproverUserID != "manager-refund-scope" || issued.Permission != permissionPOSRefund {
		t.Fatalf("unexpected approval response: %+v", issued)
	}

	tokenHash := sha256.Sum256([]byte(issued.ApprovalToken))
	var orderID, actionScope, reason string
	var consumedAt sql.NullString
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT order_id,action_scope,reason,consumed_at
		FROM pos_manager_approvals
		WHERE token_hash=?`, tokenHash[:]).Scan(&orderID, &actionScope, &reason, &consumedAt); err != nil {
		t.Fatalf("read issued approval: %v", err)
	}
	if orderID != "order-refund-scope" || actionScope != approvalActionRefundPartial || reason != "customer returned one line" || consumedAt.Valid {
		t.Fatalf("unexpected persisted approval order=%q action=%q reason=%q consumed=%v", orderID, actionScope, reason, consumedAt.Valid)
	}

	if _, err := s.consumeManagerApprovalForRefundAction(ctx, issued.ApprovalToken, cashier.UserID, orderID, approvalActionRefundFull); err == nil {
		t.Fatal("partial-refund approval authorized a full refund")
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT consumed_at FROM pos_manager_approvals WHERE token_hash=?`, tokenHash[:]).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt.Valid {
		t.Fatal("wrong-action attempt burned the issued approval")
	}

	approval, err := s.consumeManagerApprovalForRefundAction(ctx, issued.ApprovalToken, cashier.UserID, orderID, approvalActionRefundPartial)
	if err != nil {
		t.Fatalf("consume matching partial-refund approval: %v", err)
	}
	if approval.ApproverUserID != "manager-refund-scope" || approval.Reason != "customer returned one line" {
		t.Fatalf("unexpected consumed approval: %+v", approval)
	}
	if _, err := s.consumeManagerApprovalForRefundAction(ctx, issued.ApprovalToken, cashier.UserID, orderID, approvalActionRefundPartial); err == nil {
		t.Fatal("issued approval remained reusable after one matching consumption")
	}
}

func refundApprovalTestKeys(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
}

func refundApprovalTestGrant(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "pos-offline-v1"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := h + "." + p
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}
