package server

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func applyCatalogMessage(t *testing.T, service *inbox.Service, id, typ string, payload any) {
    t.Helper()
    raw, err := json.Marshal(payload); if err != nil { t.Fatal(err) }
    if err := service.Apply(context.Background(), inbox.Message{ID:id,Type:typ,SchemaVersion:1,Source:"central",Payload:raw}); err != nil { t.Fatalf("apply %s: %v", typ, err) }
}

func TestCompletedSaleCommitsReceiptInventoryAndOutboxTogether(t *testing.T) {
    ctx := context.Background()
    db := testutil.OpenDatabase(t)
    deviceService := device.New(db)
    if _, err := deviceService.EnsureInstallation(ctx); err != nil { t.Fatal(err) }
    if _, err := deviceService.ApplyRegistration(ctx, device.Registration{StoreID:"store-1",TerminalID:"terminal-1"}); err != nil { t.Fatal(err) }

    inboxService := inbox.New(db)
    applyCatalogMessage(t, inboxService, "product-1", "catalog.product.upsert", map[string]any{
        "id":"1","name":"Milk","unit_of_measure":"unit","is_active":true,"allow_manual_price":false,"track_inventory":true,"version":1,
    })
    applyCatalogMessage(t, inboxService, "price-1", "catalog.price.upsert", map[string]any{
        "id":"price-1","product_id":"1","store_id":"store-1","currency":"INR","amount_minor":12500,"tax_inclusive":true,"priority":100,"version":1,
    })

    catalogRepo := catalog.NewRepository(db)
    orderService := orders.New(db, catalogRepo)
    paymentService := payments.New(db)
    inventoryService := inventory.New(db)
    receiptService := receipts.New(db)

    order, err := orderService.Create(ctx, orders.CreateInput{
        ClientOrderID:"client-order-1", StoreID:"store-1", TerminalID:strptr("terminal-1"), Currency:"INR",
        Items:[]orders.ItemInput{{ProductID:"1",QuantityMilli:1000,DiscountMinor:0,TaxMinor:0}},
    })
    if err != nil { t.Fatalf("create order: %v", err) }
    if _, _, err := paymentService.Create(ctx, order.ID, payments.CreateInput{ClientPaymentID:"client-payment-1",Mode:"cash",AmountMinor:order.TotalMinor,Currency:"INR",Status:"captured"}); err != nil { t.Fatalf("create payment: %v", err) }

    app := New(config.Config{Environment:"test",ListenAddress:"127.0.0.1:0"}, db, deviceService, catalogRepo, customer.NewRepository(db), orderService, paymentService, inventoryService, receiptService)
    req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+order.ID+"/complete", bytes.NewReader(nil))
    res := httptest.NewRecorder()
    app.httpServer.Handler.ServeHTTP(res, req)
    if res.Code != http.StatusOK { t.Fatalf("complete status=%d body=%s", res.Code, res.Body.String()) }

    var completedAt string
    if err := db.SQL().QueryRow(`SELECT completed_at FROM sales_orders WHERE id=?`, order.ID).Scan(&completedAt); err != nil { t.Fatal(err) }
    if completedAt == "" { t.Fatal("order was not completed") }

    var receiptsCount, movementCount, outboxCount int
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM receipts WHERE order_id=?`, order.ID).Scan(&receiptsCount); err != nil { t.Fatal(err) }
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE reference_type='sale_order' AND reference_id=?`, order.ID).Scan(&movementCount); err != nil { t.Fatal(err) }
    if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='sales_order' AND aggregate_id=? AND event_type='sale.completed' AND status='pending'`, order.ID).Scan(&outboxCount); err != nil { t.Fatal(err) }
    if receiptsCount != 1 || movementCount != 1 || outboxCount != 1 { t.Fatalf("receipt=%d movements=%d outbox=%d", receiptsCount, movementCount, outboxCount) }
}

func strptr(v string) *string { return &v }
