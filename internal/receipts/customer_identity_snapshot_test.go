package receipts

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestOfflineCustomerIdentityIsFrozenInReceiptAndSaleCompletedOutbox(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	customers := customer.NewRepository(db)
	phone := "9000011111"
	email := "offline.sale@example.test"
	code := "CUST-OFFLINE-SALE-001"
	taxID := "GST-OFFLINE-SALE-001"
	createdCustomer, err := customers.Create(ctx, customer.UpsertInput{
		CustomerCode: &code,
		Name:         "Offline Sale Customer",
		Phone:        &phone,
		Email:        &email,
		TaxID:        &taxID,
		Currency:     "INR",
	})
	if err != nil {
		t.Fatalf("create offline customer: %v", err)
	}
	if createdCustomer.SyncState != "pending" {
		t.Fatalf("expected offline customer pending sync, got %q", createdCustomer.SyncState)
	}

	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(
			id,name,unit_of_measure,is_active,allow_manual_price,track_inventory,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		"offline-customer-product", "Offline Customer Product", "unit", 1, 0, 0, 1, "2026-08-21T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog product: %v", err)
	}
	_, err = db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(
			id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"offline-customer-price", "offline-customer-product", "store-1", "INR", 12500, 1, 100, 1, "2026-08-21T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert catalog price: %v", err)
	}

	orderService := orders.New(db, catalog.NewRepository(db))
	customerID := orders.ExternalID(createdCustomer.ID)
	order, err := orderService.Create(orders.WithCreatorUserID(ctx, "cashier-central-1"), orders.CreateInput{
		ClientOrderID: "offline-customer-order-1",
		StoreID:       "store-1",
		CustomerID:    &customerID,
		Currency:      "INR",
		Items: []orders.ItemInput{{
			ProductID:     orders.ExternalID("offline-customer-product"),
			QuantityMilli: 1000,
		}},
	})
	if err != nil {
		t.Fatalf("create order for offline customer: %v", err)
	}
	if order.CustomerID == nil || *order.CustomerID != createdCustomer.ID {
		t.Fatalf("order lost offline customer identity: %#v", order.CustomerID)
	}

	completedAt := time.Now().UTC().Format(time.RFC3339Nano)
	order.CompletedAt = &completedAt
	order.Version = 2
	order.CompletedByUserID = stringPtr("cashier-central-2")

	receiptService := New(db)
	if err := db.WithTx(ctx, func(tx interfaceTx) error { return nil }); err != nil {
		t.Fatal(err)
	}

	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return receiptService.ApplyCompletionTx(ctx, tx, order)
	}); err != nil {
		t.Fatalf("apply receipt completion: %v", err)
	}

	receipt, err := receiptService.GetByOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	if receipt.CustomerID == nil || *receipt.CustomerID != createdCustomer.ID {
		t.Fatalf("receipt lost customer relationship: %#v", receipt.CustomerID)
	}
	if receipt.Snapshot.Customer == nil || receipt.Snapshot.Customer.ID != createdCustomer.ID || receipt.Snapshot.Customer.Name != "Offline Sale Customer" {
		t.Fatalf("receipt customer snapshot mismatch: %#v", receipt.Snapshot.Customer)
	}
	if receipt.Snapshot.Order.CustomerID == nil || *receipt.Snapshot.Order.CustomerID != createdCustomer.ID {
		t.Fatalf("receipt order snapshot lost customer id: %#v", receipt.Snapshot.Order.CustomerID)
	}
	if receipt.Snapshot.Order.CreatedByUserID == nil || *receipt.Snapshot.Order.CreatedByUserID != "cashier-central-1" {
		t.Fatalf("receipt order snapshot lost creator identity: %#v", receipt.Snapshot.Order.CreatedByUserID)
	}
	if receipt.Snapshot.Order.CompletedByUserID == nil || *receipt.Snapshot.Order.CompletedByUserID != "cashier-central-2" {
		t.Fatalf("receipt order snapshot lost completer identity: %#v", receipt.Snapshot.Order.CompletedByUserID)
	}

	updatedEmail := "changed.after.sale@example.test"
	_, err = customers.Update(ctx, createdCustomer.ID, customer.UpsertInput{
		CustomerCode: &code,
		Name:         "Customer Changed After Sale",
		Phone:        &phone,
		Email:        &updatedEmail,
		TaxID:        &taxID,
		Currency:     "INR",
	})
	if err != nil {
		t.Fatalf("update customer after sale: %v", err)
	}

	frozenReceipt, err := receiptService.GetByOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("read frozen receipt: %v", err)
	}
	if frozenReceipt.Snapshot.Customer == nil || frozenReceipt.Snapshot.Customer.Name != "Offline Sale Customer" || frozenReceipt.Snapshot.Customer.Email == nil || *frozenReceipt.Snapshot.Customer.Email != email {
		t.Fatalf("historical receipt customer snapshot mutated: %#v", frozenReceipt.Snapshot.Customer)
	}

	var payloadRaw string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT payload_json FROM outbox_events
		WHERE aggregate_type='sales_order' AND aggregate_id=? AND aggregate_version=2 AND event_type='sale.completed'`, order.ID).
		Scan(&payloadRaw); err != nil {
		t.Fatalf("read sale.completed outbox payload: %v", err)
	}
	var payload outbox.SaleCompletedPayload
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		t.Fatalf("decode sale.completed payload: %v", err)
	}
	if payload.Order.CustomerID == nil || *payload.Order.CustomerID != createdCustomer.ID {
		t.Fatalf("sale.completed order lost offline customer id: %#v", payload.Order.CustomerID)
	}
	var outboxReceipt Receipt
	if err := json.Unmarshal(payload.Receipt, &outboxReceipt); err != nil {
		t.Fatalf("decode outbox receipt: %v", err)
	}
	if outboxReceipt.CustomerID == nil || *outboxReceipt.CustomerID != createdCustomer.ID || outboxReceipt.Snapshot.Customer == nil || outboxReceipt.Snapshot.Customer.Name != "Offline Sale Customer" {
		t.Fatalf("sale.completed receipt lost historical customer identity: %#v", outboxReceipt)
	}

	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return receiptService.ApplyCompletionTx(ctx, tx, order)
	}); err != nil {
		t.Fatalf("retry completion receipt: %v", err)
	}
	var saleEventCount int
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE aggregate_type='sales_order' AND aggregate_id=? AND aggregate_version=2 AND event_type='sale.completed'`, order.ID).
		Scan(&saleEventCount); err != nil {
		t.Fatalf("count sale.completed events: %v", err)
	}
	if saleEventCount != 1 {
		t.Fatalf("completion retry duplicated sale.completed event: got %d want 1", saleEventCount)
	}
}

func stringPtr(v string) *string { return &v }
