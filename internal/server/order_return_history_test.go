package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/refunds"
)

type fakePartialReturnHistoryReader struct {
	items []refunds.PartialReturnLedgerRecord
	err   error
}

func (f fakePartialReturnHistoryReader) ListPartialReturns(context.Context, string) ([]refunds.PartialReturnLedgerRecord, error) {
	return f.items, f.err
}

func TestOrderReturnHistoryReturnsDurableAuditFacts(t *testing.T) {
	reader := fakePartialReturnHistoryReader{items: []refunds.PartialReturnLedgerRecord{{
		ID:               "ret-1",
		OrderID:          "ord-1",
		ApprovedByUserID: "manager-1",
		Reason:           "damaged item",
		RefundMinor:      2500,
		CreatedAt:        "2026-08-09T10:00:00Z",
		Lines: []refunds.PartialReturnLedgerLine{{
			OrderItemID: "item-1", QuantityMilli: 250, RefundMinor: 2500,
		}},
	}}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/returns", nil)
	req.SetPathValue("id", "ord-1")
	res := httptest.NewRecorder()
	handleOrderReturnHistoryWith(reader)(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var body struct {
		Count int `json:"count"`
		Items []struct {
			ReturnID         string `json:"return_id"`
			OrderID          string `json:"order_id"`
			ApprovedByUserID string `json:"approved_by_user_id"`
			Reason           string `json:"reason"`
			RefundMinor      int64  `json:"refund_minor"`
			CreatedAt        string `json:"created_at"`
			Lines []struct {
				OrderItemID   string `json:"order_item_id"`
				QuantityMilli int64  `json:"quantity_milli"`
				RefundMinor   int64  `json:"refund_minor"`
			} `json:"lines"`
		} `json:"items"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 1 || len(body.Items) != 1 || body.Items[0].ReturnID != "ret-1" || body.Items[0].OrderID != "ord-1" || body.Items[0].ApprovedByUserID != "manager-1" || body.Items[0].Reason != "damaged item" || body.Items[0].RefundMinor != 2500 || body.Items[0].CreatedAt != "2026-08-09T10:00:00Z" {
		t.Fatalf("body=%+v", body)
	}
	if len(body.Items[0].Lines) != 1 || body.Items[0].Lines[0].OrderItemID != "item-1" || body.Items[0].Lines[0].QuantityMilli != 250 || body.Items[0].Lines[0].RefundMinor != 2500 {
		t.Fatalf("lines=%+v", body.Items[0].Lines)
	}
}

func TestOrderReturnHistoryReturnsEmptyListForValidOrderWithoutReturns(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/returns", nil)
	req.SetPathValue("id", "ord-1")
	res := httptest.NewRecorder()
	handleOrderReturnHistoryWith(fakePartialReturnHistoryReader{items: []refunds.PartialReturnLedgerRecord{}})(res, req)

	if res.Code != http.StatusOK || res.Body.String() != "{\"count\":0,\"items\":[]}\n" {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestOrderReturnHistoryMapsValidationAndMissingOrder(t *testing.T) {
	for name, err := range map[string]error{
		"invalid": refunds.ErrInvalidPartialReturn,
		"missing": orders.ErrNotFound,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/returns", nil)
			req.SetPathValue("id", "ord-1")
			res := httptest.NewRecorder()
			handleOrderReturnHistoryWith(fakePartialReturnHistoryReader{err: err})(res, req)
			if name == "invalid" && res.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			if name == "missing" && res.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
		})
	}
}

func TestOrderReturnHistoryMapsUnexpectedFailure(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/returns", nil)
	req.SetPathValue("id", "ord-1")
	res := httptest.NewRecorder()
	handleOrderReturnHistoryWith(fakePartialReturnHistoryReader{err: errors.New("db unavailable")})(res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestOrderReturnHistoryRequiresOrdersReadPermission(t *testing.T) {
	reader := fakePartialReturnHistoryReader{items: []refunds.PartialReturnLedgerRecord{}}
	handler := requirePermission("orders:read", handleOrderReturnHistoryWith(reader))

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/returns", nil)
	unauthorized.SetPathValue("id", "ord-1")
	unauthorized = unauthorized.WithContext(context.WithValue(unauthorized.Context(), authContextKey{}, LocalUserContext{UserID: "cashier-1", Permissions: []string{"pos:sale"}}))
	unauthorizedRes := httptest.NewRecorder()
	handler(unauthorizedRes, unauthorized)
	if unauthorizedRes.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status=%d body=%s", unauthorizedRes.Code, unauthorizedRes.Body.String())
	}

	authorized := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ord-1/returns", nil)
	authorized.SetPathValue("id", "ord-1")
	authorized = authorized.WithContext(context.WithValue(authorized.Context(), authContextKey{}, LocalUserContext{UserID: "cashier-1", Permissions: []string{"orders:read"}}))
	authorizedRes := httptest.NewRecorder()
	handler(authorizedRes, authorized)
	if authorizedRes.Code != http.StatusOK {
		t.Fatalf("authorized status=%d body=%s", authorizedRes.Code, authorizedRes.Body.String())
	}
}
