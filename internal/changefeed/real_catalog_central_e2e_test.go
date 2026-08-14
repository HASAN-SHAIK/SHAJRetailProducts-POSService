package changefeed

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
)

func mutateCatalogState(t *testing.T, centralURL, state string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, centralURL+"/e2e/catalog/"+state, nil)
	if err != nil { t.Fatal(err) }
	resp, err := http.DefaultClient.Do(req)
	if err != nil { t.Fatalf("mutate Central catalog to %s: %v", state, err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent { t.Fatalf("mutate Central catalog to %s returned %d", state, resp.StatusCode) }
}

func TestRealCentralCatalogConvergenceE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" { t.Skip("POS_E2E_CENTRAL_URL is required for real Central catalog E2E") }
	tenantID := os.Getenv("POS_E2E_TENANT_ID")
	syncToken := os.Getenv("POS_E2E_SYNC_TOKEN")
	deviceID := os.Getenv("POS_E2E_DEVICE_ID")
	if tenantID == "" || syncToken == "" || deviceID == "" { t.Fatal("POS_E2E_TENANT_ID, POS_E2E_SYNC_TOKEN and POS_E2E_DEVICE_ID are required") }

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "catalog-e2e.db")
	db, err := database.Open(ctx, dbPath)
	if err != nil { t.Fatalf("open POS database: %v", err) }
	if err := db.Migrate(ctx); err != nil { _ = db.Close(); t.Fatalf("migrate POS database: %v", err) }
	puller := New(db, inbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, time.Second)

	more, err := puller.pullOnce(ctx)
	if err != nil { _ = db.Close(); t.Fatalf("pull initial Central catalog changes: %v", err) }
	if more { _ = db.Close(); t.Fatal("unexpected additional initial catalog page") }

	repo := catalog.NewRepository(db)
	const storeID = "11111111-1111-1111-1111-111111111111"
	const oldCategoryID = "Fresh%20Produce%20%26%20Dairy"
	byBarcode, err := repo.GetByBarcode(ctx, "8901234567890", storeID)
	if err != nil { _ = db.Close(); t.Fatalf("initial offline barcode lookup: %v", err) }
	if byBarcode.ID != "101" || byBarcode.Name != "Fresh Milk" || byBarcode.CategoryID == nil || *byBarcode.CategoryID != oldCategoryID { _ = db.Close(); t.Fatalf("unexpected initial product: %#v", byBarcode) }
	if byBarcode.Price == nil || byBarcode.Price.AmountMinor != 6550 || byBarcode.Price.Currency != "INR" { _ = db.Close(); t.Fatalf("initial effective price mismatch: %#v", byBarcode.Price) }
	categories, err := repo.ListCategories(ctx)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if len(categories) != 1 || categories[0].ID != oldCategoryID { _ = db.Close(); t.Fatalf("unexpected initial categories: %#v", categories) }

	mutateCatalogState(t, centralURL, "rename")
	more, err = puller.pullOnce(ctx)
	if err != nil { _ = db.Close(); t.Fatalf("pull renamed Central category: %v", err) }
	if more { _ = db.Close(); t.Fatal("unexpected page after category rename") }
	categories, err = repo.ListCategories(ctx)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if len(categories) != 1 || categories[0].ID != "New%20Dairy" || categories[0].Name != "New Dairy" { _ = db.Close(); t.Fatalf("category rename left stale/incorrect categories: %#v", categories) }
	byBarcode, err = repo.GetByBarcode(ctx, "8901234567890", storeID)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if byBarcode.CategoryID == nil || *byBarcode.CategoryID != "New%20Dairy" { _ = db.Close(); t.Fatalf("product did not converge to renamed category: %#v", byBarcode.CategoryID) }

	mutateCatalogState(t, centralURL, "clear")
	more, err = puller.pullOnce(ctx)
	if err != nil { _ = db.Close(); t.Fatalf("pull cleared Central category: %v", err) }
	if more { _ = db.Close(); t.Fatal("unexpected page after category removal") }
	categories, err = repo.ListCategories(ctx)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if len(categories) != 0 { _ = db.Close(); t.Fatalf("removed Central category remained visible in POS: %#v", categories) }

	var appliedBeforeRestart int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE status='applied'`).Scan(&appliedBeforeRestart); err != nil { _ = db.Close(); t.Fatal(err) }
	if err := db.Close(); err != nil { t.Fatalf("close POS database before restart: %v", err) }

	db, err = database.Open(ctx, dbPath)
	if err != nil { t.Fatalf("reopen POS database: %v", err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate restarted POS database: %v", err) }
	repo = catalog.NewRepository(db)
	byBarcode, err = repo.GetByBarcode(ctx, "8901234567890", storeID)
	if err != nil { t.Fatalf("offline barcode lookup after restart: %v", err) }
	if byBarcode.CategoryID != nil { t.Fatalf("cleared category did not persist across restart: %#v", byBarcode.CategoryID) }
	matches, err := repo.Search(ctx, "fresh milk", storeID, 20)
	if err != nil || len(matches) != 1 || matches[0].ID != "101" { t.Fatalf("offline name lookup after restart failed: matches=%#v err=%v", matches, err) }

	restartedPuller := New(db, inbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, time.Second)
	more, err = restartedPuller.pullOnce(ctx)
	if err != nil { t.Fatalf("replay real Central catalog changes: %v", err) }
	if more { t.Fatal("unexpected page on replay") }
	var afterReplay int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE status='applied'`).Scan(&afterReplay); err != nil { t.Fatal(err) }
	if afterReplay != appliedBeforeRestart { t.Fatalf("catalog replay duplicated inbox facts: before=%d after=%d", appliedBeforeRestart, afterReplay) }
}
