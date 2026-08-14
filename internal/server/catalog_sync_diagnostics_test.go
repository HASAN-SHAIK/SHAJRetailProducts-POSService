package server

import (
	"context"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/testutil"
)

func TestCatalogInboxFailureIsVisibleToSupportDiagnostics(t *testing.T) {
	db := testutil.OpenDatabase(t)
	ctx := context.Background()

	_, err := db.SQL().ExecContext(ctx, `
		INSERT INTO inbox_messages(
			message_id,message_type,schema_version,source,payload_json,status,attempt_count,received_at,last_error
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"catalog-product-101-v7",
		"catalog.product.upsert",
		1,
		"central",
		`{"id":"101","name":"Milk","version":7}`,
		"failed",
		3,
		"2026-08-14T09:00:00Z",
		"invalid_product_payload",
	)
	if err != nil {
		t.Fatalf("insert failed catalog inbox message: %v", err)
	}

	s := &Server{db: db}
	items, err := s.loadInboxDiagnostics(ctx, 10)
	if err != nil {
		t.Fatalf("load inbox diagnostics: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one catalog diagnostic item, got %d", len(items))
	}

	item := items[0]
	if item.MessageID != "catalog-product-101-v7" || item.MessageType != "catalog.product.upsert" {
		t.Fatalf("catalog message identity missing from diagnostics: %+v", item)
	}
	if item.Status != "failed" || item.AttemptCount != 3 || item.LastError != "invalid_product_payload" {
		t.Fatalf("catalog failure evidence missing from diagnostics: %+v", item)
	}
	if item.Source != "central" || string(item.Payload) == "" {
		t.Fatalf("catalog source/payload missing from diagnostics: %+v", item)
	}
}
