package server

import (
	"os"
	"strings"
	"testing"
)

func TestRefundReconciliationRouteIsOrdersReadProtected(t *testing.T) {
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}

	const route = `mux.HandleFunc("GET /api/v1/orders/{id}/reconciliation", requirePermission("orders:read", s.handleOrderRefundReconciliation))`
	if !strings.Contains(string(source), route) {
		t.Fatalf("refund reconciliation route is missing or not protected by orders:read")
	}
}
