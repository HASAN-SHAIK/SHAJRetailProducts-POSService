package refunds

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
)

func TestReturnPartialEmitsItemLevelFactAndFinalOperationAlsoEmitsSaleReturned(t *testing.T) {
	ctx := context.Background()
	db := openRefundDB(t)
	seedCompletedSale(t, db, true)
	svc, paymentService := newRefundService(db)
	if _, _, err := paymentService.Create(ctx, "ord-refund-full", payments.CreateInput{
		ClientPaymentID: "capture-1", Mode: "cash", AmountMinor: 10000, Currency: "INR", Status: "captured",
	}); err != nil {
		t.Fatal(err)
	}

	firstOrder, _, err := svc.ReturnPartial(ctx, PartialReturnInput{
		ReturnID: "ret-event-1", OrderID: "ord-refund-full", ApprovedByUserID: "manager-1", Reason: "first item return",
		Lines: []PartialReturnLineInput{{OrderItemID: "item-refund-full", QuantityMilli: 250}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var payloadRaw string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT payload_json FROM outbox_events
		WHERE aggregate_id='ord-refund-full' AND event_type='sale.partial_returned' AND aggregate_version=?`, firstOrder.Version).Scan(&payloadRaw); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ReturnID string `json:"return_id"`
		RefundMinor int64 `json:"refund_minor"`
		ApprovedByUserID string `json:"approved_by_user_id"`
		ApprovalReason string `json:"approval_reason"`
		Order struct {
			Status string `json:"status"`
			Version int `json:"version"`
		} `json:"order"`
		Lines []struct {
			OrderItemID string `json:"order_item_id"`
			QuantityMilli int64 `json:"quantity_milli"`
			RefundMinor int64 `json:"refund_minor"`
		} `json:"lines"`
	}
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ReturnID != "ret-event-1" || payload.RefundMinor != 2500 || payload.ApprovedByUserID != "manager-1" || payload.ApprovalReason != "first item return" {
		t.Fatalf("partial event audit mismatch %+v", payload)
	}
	if payload.Order.Status != "completed" || payload.Order.Version != firstOrder.Version || len(payload.Lines) != 1 || payload.Lines[0].OrderItemID != "item-refund-full" || payload.Lines[0].QuantityMilli != 250 || payload.Lines[0].RefundMinor != 2500 {
		t.Fatalf("partial event facts mismatch %+v", payload)
	}

	finalInput := PartialReturnInput{
		ReturnID: "ret-event-2", OrderID: "ord-refund-full", ApprovedByUserID: "manager-2", Reason: "return remainder",
		Lines: []PartialReturnLineInput{{OrderItemID: "item-refund-full", QuantityMilli: 750}},
	}
	finalOrder, _, err := svc.ReturnPartial(ctx, finalInput)
	if err != nil {
		t.Fatal(err)
	}

	var partialEvents, returnedEvents int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-full' AND event_type='sale.partial_returned'`).Scan(&partialEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-full' AND event_type='sale.returned'`).Scan(&returnedEvents); err != nil {
		t.Fatal(err)
	}
	if partialEvents != 2 || returnedEvents != 1 {
		t.Fatalf("partialEvents=%d returnedEvents=%d", partialEvents, returnedEvents)
	}

	var finalStatus string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT json_extract(payload_json,'$.order.status') FROM outbox_events
		WHERE aggregate_id='ord-refund-full' AND event_type='sale.partial_returned' AND aggregate_version=?`, finalOrder.Version).Scan(&finalStatus); err != nil {
		t.Fatal(err)
	}
	if finalStatus != "returned" {
		t.Fatalf("final partial event status=%q", finalStatus)
	}

	if _, _, err := svc.ReturnPartial(ctx, finalInput); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id='ord-refund-full' AND event_type='sale.partial_returned'`).Scan(&partialEvents); err != nil {
		t.Fatal(err)
	}
	if partialEvents != 2 {
		t.Fatalf("replay emitted duplicate partial events=%d", partialEvents)
	}
}
