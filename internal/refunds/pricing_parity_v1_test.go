package refunds

import (
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func TestV1PartialReturnsPreserveImmutableTaxedDiscountedLineTotal(t *testing.T) {
	completedAt := "2026-08-15T00:00:00Z"
	order := orders.Order{
		ID: "ord-pricing-parity",
		Status: "paid",
		CompletedAt: &completedAt,
		SubtotalMinor: 12500,
		DiscountMinor: 500,
		TaxMinor: 2160,
		TotalMinor: 14160,
		Items: []orders.Item{{
			ID: "item-pricing-parity",
			QuantityMilli: 1000,
			UnitPriceMinor: 12500,
			DiscountMinor: 500,
			TaxMinor: 2160,
			LineTotalMinor: 14160,
		}},
	}

	history := map[string]PartialReturnHistory{}
	first, err := PlanPartialReturn(order, []PartialReturnLineInput{{OrderItemID: "item-pricing-parity", QuantityMilli: 333}}, history)
	if err != nil { t.Fatal(err) }
	if first.RefundMinor != 4715 || first.FullRemaining {
		t.Fatalf("first partial refund=%d full=%v want=4715/false", first.RefundMinor, first.FullRemaining)
	}

	history["item-pricing-parity"] = PartialReturnHistory{QuantityMilli: 333, RefundMinor: first.RefundMinor}
	second, err := PlanPartialReturn(order, []PartialReturnLineInput{{OrderItemID: "item-pricing-parity", QuantityMilli: 333}}, history)
	if err != nil { t.Fatal(err) }
	if second.RefundMinor != 4715 || second.FullRemaining {
		t.Fatalf("second partial refund=%d full=%v want=4715/false", second.RefundMinor, second.FullRemaining)
	}

	history["item-pricing-parity"] = PartialReturnHistory{QuantityMilli: 666, RefundMinor: first.RefundMinor + second.RefundMinor}
	final, err := PlanPartialReturn(order, []PartialReturnLineInput{{OrderItemID: "item-pricing-parity", QuantityMilli: 334}}, history)
	if err != nil { t.Fatal(err) }
	if final.RefundMinor != 4730 || !final.FullRemaining {
		t.Fatalf("final partial refund=%d full=%v want=4730/true", final.RefundMinor, final.FullRemaining)
	}

	if got := first.RefundMinor + second.RefundMinor + final.RefundMinor; got != order.Items[0].LineTotalMinor {
		t.Fatalf("cumulative refund=%d want immutable line total=%d", got, order.Items[0].LineTotalMinor)
	}
}
