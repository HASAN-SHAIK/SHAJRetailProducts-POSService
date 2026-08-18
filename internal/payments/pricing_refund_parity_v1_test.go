package payments

import (
	"context"
	"testing"
)

func TestV1FullRefundReversesImmutableTaxedDiscountedSaleTotal(t *testing.T) {
	db := openPaymentsDB(t)
	defer db.Close()
	seedPaymentOrder(t, db, "ord-pricing-refund", "client-pricing-refund", 14160)
	service := New(db)
	ctx := context.Background()

	capture, capturedSummary, err := service.Create(ctx, "ord-pricing-refund", CreateInput{
		ClientPaymentID: "capture-pricing-refund",
		Mode: "cash",
		AmountMinor: 14160,
		Currency: "INR",
		Status: "captured",
	})
	if err != nil { t.Fatal(err) }
	if capture.AmountMinor != 14160 || capturedSummary.PaidMinor != 14160 || capturedSummary.BalanceMinor != 0 {
		t.Fatalf("unexpected capture=%#v summary=%#v", capture, capturedSummary)
	}

	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil { t.Fatal(err) }
	refund, refundedSummary, err := service.CreateRefundTx(ctx, tx, "ord-pricing-refund", CreateInput{
		ClientPaymentID: "refund-pricing-refund",
		Mode: "cash",
		AmountMinor: 14160,
		Currency: "INR",
	})
	if err != nil { _ = tx.Rollback(); t.Fatal(err) }
	if refund.AmountMinor != 14160 || refund.Direction != "out" || refund.Status != "refunded" {
		_ = tx.Rollback()
		t.Fatalf("unexpected refund=%#v", refund)
	}
	if refundedSummary.PaidMinor != 0 || refundedSummary.BalanceMinor != 14160 {
		_ = tx.Rollback()
		t.Fatalf("unexpected refunded summary=%#v", refundedSummary)
	}
	if err := tx.Commit(); err != nil { t.Fatal(err) }

	assertPaymentCount(t, db, `SELECT COUNT(*) FROM payments WHERE order_id=? AND direction='out' AND amount_minor=14160 AND status='refunded'`, "ord-pricing-refund", 1)
}
