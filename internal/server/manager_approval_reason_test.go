package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSensitiveManagerApprovalRequiresReasonBeforeIssuance(t *testing.T) {
	tests := []struct {
		name       string
		permission string
		reason     string
	}{
		{name: "void missing reason", permission: permissionPOSVoid, reason: ""},
		{name: "refund whitespace reason", permission: permissionPOSRefund, reason: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{}
			body := `{"manager_user_id":"manager-1","pin":"1234","permission":"` + tt.permission + `","reason":"` + tt.reason + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/approvals", strings.NewReader(body))
			req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, LocalUserContext{
				UserID:      "cashier-1",
				TenantID:    "tenant-1",
				BranchID:    "branch-1",
				Permissions: []string{permissionPOSSale},
			}))
			res := httptest.NewRecorder()

			s.handleManagerApproval(res, req)

			if res.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s want=%d", res.Code, res.Body.String(), http.StatusBadRequest)
			}
			if !strings.Contains(res.Body.String(), "invalid_approval_payload") {
				t.Fatalf("body=%s want invalid_approval_payload", res.Body.String())
			}
		})
	}
}

func TestApprovalReasonPolicyLeavesDiscountCompatibilityUnchanged(t *testing.T) {
	if approvalRequiresReason(permissionPOSDiscount) {
		t.Fatal("discount approvals unexpectedly require an audit reason")
	}
	if !approvalRequiresReason(permissionPOSVoid) {
		t.Fatal("void approvals must require an audit reason")
	}
	if !approvalRequiresReason(permissionPOSRefund) {
		t.Fatal("refund approvals must require an audit reason")
	}
}
