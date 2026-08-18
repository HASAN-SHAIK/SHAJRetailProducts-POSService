package orders

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestV1OrderPreservesSaleTimeCategorySnapshot(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_categories(id,name,sort_order,is_active,version,updated_at)
		VALUES(?,?,?,?,?,?)`,
		"cat-beverages", "Beverages", 1, 1, 1, "2026-08-18T10:00:00Z",
	); err != nil {
		t.Fatalf("insert category: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_products(id,category_id,name,is_active,allow_manual_price,track_inventory,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		"101", "cat-beverages", "Cola", 1, 0, 0, 1, "2026-08-18T10:00:00Z",
	); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		INSERT INTO catalog_prices(id,product_id,store_id,currency,amount_minor,tax_inclusive,priority,version,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"price-101", "101", "store-1", "INR", 10000, 1, 100, 1, "2026-08-18T10:00:00Z",
	); err != nil {
		t.Fatalf("insert price: %v", err)
	}

	service := New(db, catalog.NewRepository(db))
	order, err := service.Create(ctx, CreateInput{
		ClientOrderID: "category-snapshot-order",
		StoreID:       "store-1",
		Currency:      "INR",
		Items: []ItemInput{{ProductID: ExternalID("101"), QuantityMilli: 1000}},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if len(order.Items) != 1 || order.Items[0].CategoryIDSnapshot == nil || *order.Items[0].CategoryIDSnapshot != "cat-beverages" {
		t.Fatalf("category id snapshot missing: %+v", order.Items)
	}
	if order.Items[0].CategoryNameSnapshot == nil || *order.Items[0].CategoryNameSnapshot != "Beverages" {
		t.Fatalf("category name snapshot missing: %+v", order.Items[0].CategoryNameSnapshot)
	}

	if _, err := db.SQL().ExecContext(ctx, `UPDATE catalog_categories SET name=?, version=version+1 WHERE id=?`, "Snacks", "cat-beverages"); err != nil {
		t.Fatalf("rename current catalog category: %v", err)
	}

	reloaded, err := service.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if reloaded.Items[0].CategoryNameSnapshot == nil || *reloaded.Items[0].CategoryNameSnapshot != "Beverages" {
		t.Fatalf("historical category was rewritten by current catalog: %+v", reloaded.Items[0])
	}

	raw, err := json.Marshal(reloaded)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	payload := string(raw)
	if !strings.Contains(payload, `"category_id":"cat-beverages"`) || !strings.Contains(payload, `"category_name":"Beverages"`) {
		t.Fatalf("durable order payload lost category snapshot: %s", payload)
	}
}
