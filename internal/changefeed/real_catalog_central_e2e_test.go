package changefeed

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
)

func TestRealCentralCatalogConvergenceE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" {
		t.Skip("POS_E2E_CENTRAL_URL is required for real Central catalog E2E")
	}
	tenantID := os.Getenv("POS_E2E_TENANT_ID")
	syncToken := os.Getenv("POS_E2E_SYNC_TOKEN")
	deviceID := os.Getenv("POS_E2E_DEVICE_ID")
	if tenantID == "" || syncToken == "" || deviceID == "" {
		t.Fatal("POS_E2E_TENANT_ID, POS_E2E_SYNC_TOKEN and POS_E2E_DEVICE_ID are required")
	}

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "catalog-e2e.db")
	db, err := database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open POS database: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("migrate POS database: %v", err)
	}

	puller := New(db, inbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, time.Second)
	more, err := puller.pullOnce(ctx)
	if err != nil {
		_ = db.Close()
		t.Fatalf("pull real Central catalog changes: %v", err)
	}
	if more {
		_ = db.Close()
		t.Fatal("unexpected additional catalog page")
	}

	var applied int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE status='applied'`).Scan(&applied); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if applied != 4 {
		_ = db.Close()
		t.Fatalf("expected category/product/barcode/price messages, got %d applied inbox messages", applied)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close POS database before restart: %v", err)
	}

	db, err = database.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen POS database: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate restarted POS database: %v", err)
	}

	repo := catalog.NewRepository(db)
	const storeID = "11111111-1111-1111-1111-111111111111"
	byBarcode, err := repo.GetByBarcode(ctx, "8901234567890", storeID)
	if err != nil {
		t.Fatalf("offline barcode lookup after restart: %v", err)
	}
	if byBarcode.ID != "101" || byBarcode.Name != "Fresh Milk" {
		t.Fatalf("unexpected barcode product: %#v", byBarcode)
	}
	const categoryID = "Fresh%20Produce%20%26%20Dairy"
	if byBarcode.CategoryID == nil || *byBarcode.CategoryID != categoryID {
		t.Fatalf("category identity mismatch: %#v", byBarcode.CategoryID)
	}
	if byBarcode.Price == nil || byBarcode.Price.AmountMinor != 6550 || byBarcode.Price.Currency != "INR" {
		t.Fatalf("effective offline price mismatch: %#v", byBarcode.Price)
	}

	matches, err := repo.Search(ctx, "fresh milk", storeID, 20)
	if err != nil {
		t.Fatalf("offline name search after restart: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != "101" {
		t.Fatalf("expected one synchronized name match, got %#v", matches)
	}
	categories, err := repo.ListCategories(ctx)
	if err != nil {
		t.Fatalf("offline category listing: %v", err)
	}
	if len(categories) != 1 || categories[0].ID != categoryID || categories[0].Name != "Fresh Produce & Dairy" {
		t.Fatalf("unexpected synchronized categories: %#v", categories)
	}

	// Replaying after restart must use the persisted Central cursor and produce no new inbox facts.
	restartedPuller := New(db, inbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, time.Second)
	more, err = restartedPuller.pullOnce(ctx)
	if err != nil {
		t.Fatalf("replay real Central catalog changes: %v", err)
	}
	if more {
		t.Fatal("unexpected page on replay")
	}
	var afterReplay int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE status='applied'`).Scan(&afterReplay); err != nil {
		t.Fatal(err)
	}
	if afterReplay != applied {
		t.Fatalf("catalog replay duplicated inbox facts: before=%d after=%d", applied, afterReplay)
	}
}
