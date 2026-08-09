package refunds

import (
	"errors"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
)

func completedOrderForPartialPlan() orders.Order {
	completed := "2026-08-09T00:00:00Z"
	return orders.Order{
		ID: "ord-1",
		Status: "paid",
		CompletedAt: &completed,
		Items: []orders.Item{
			{ID: "item-1", QuantityMilli: 3000, LineTotalMinor: 1000},
			{ID: "item-2", QuantityMilli: 2000, LineTotalMinor: 501},
		},
	}
}

func TestPlanPartialReturnCumulativeRoundingConvergesExactly(t *testing.T) {
	order := completedOrderForPartialPlan()

	first, err := PlanPartialReturn(order, []PartialReturnLineInput{{OrderItemID: "item-1", QuantityMilli: 1000}}, nil)
	if err != nil { t.Fatal(err) }
	if first.RefundMinor != 333 { t.Fatalf("first refund=%d want 333", first.RefundMinor) }
	if first.FullRemaining { t.Fatal("first partial return must not be full remaining") }

	history := map[string]PartialReturnHistory{
		"item-1": {QuantityMilli: 1000, RefundMinor: first.RefundMinor},
	}
	second, err := PlanPartialReturn(order, []PartialReturnLineInput{{OrderItemID: "item-1", QuantityMilli: 2000}}, history)
	if err != nil { t.Fatal(err) }
	if second.RefundMinor != 667 { t.Fatalf("second refund=%d want 667", second.RefundMinor) }
	if first.RefundMinor+second.RefundMinor != 1000 {
		t.Fatalf("cumulative refund=%d want exact line total 1000", first.RefundMinor+second.RefundMinor)
	}
}

func TestPlanPartialReturnRejectsOverReturnAndDuplicateLine(t *testing.T) {
	order := completedOrderForPartialPlan()
	history := map[string]PartialReturnHistory{"item-1": {QuantityMilli: 2500, RefundMinor: 833}}

	_, err := PlanPartialReturn(order, []PartialReturnLineInput{{OrderItemID: "item-1", QuantityMilli: 501}}, history)
	if !errors.Is(err, ErrReturnQuantityExceeded) {
		t.Fatalf("over-return error=%v want ErrReturnQuantityExceeded", err)
	}

	_, err = PlanPartialReturn(order, []PartialReturnLineInput{
		{OrderItemID: "item-1", QuantityMilli: 100},
		{OrderItemID: "item-1", QuantityMilli: 100},
	}, nil)
	if !errors.Is(err, ErrInvalidPartialReturn) {
		t.Fatalf("duplicate error=%v want ErrInvalidPartialReturn", err)
	}
}

func TestPlanPartialReturnDetectsFullRemainingAcrossAllLines(t *testing.T) {
	order := completedOrderForPartialPlan()
	history := map[string]PartialReturnHistory{
		"item-1": {QuantityMilli: 1000, RefundMinor: 333},
		"item-2": {QuantityMilli: 1000, RefundMinor: 250},
	}

	plan, err := PlanPartialReturn(order, []PartialReturnLineInput{
		{OrderItemID: "item-1", QuantityMilli: 2000},
		{OrderItemID: "item-2", QuantityMilli: 1000},
	}, history)
	if err != nil { t.Fatal(err) }
	if !plan.FullRemaining { t.Fatal("expected the request to consume every remaining sale quantity") }
	if plan.RefundMinor != 918 {
		t.Fatalf("refund=%d want 918 (667 + 251 exact final allocations)", plan.RefundMinor)
	}
}

func TestPlanPartialReturnRejectsImpossibleHistoryAndLifecycle(t *testing.T) {
	order := completedOrderForPartialPlan()
	_, err := PlanPartialReturn(order, []PartialReturnLineInput{{OrderItemID: "item-1", QuantityMilli: 100}}, map[string]PartialReturnHistory{
		"item-1": {QuantityMilli: 1000, RefundMinor: 900},
	})
	if !errors.Is(err, ErrInvalidPartialReturn) {
		t.Fatalf("history error=%v want ErrInvalidPartialReturn", err)
	}

	order.Status = "returned"
	_, err = PlanPartialReturn(order, []PartialReturnLineInput{{OrderItemID: "item-1", QuantityMilli: 100}}, nil)
	if !errors.Is(err, ErrInvalidPartialReturn) {
		t.Fatalf("lifecycle error=%v want ErrInvalidPartialReturn", err)
	}
}
