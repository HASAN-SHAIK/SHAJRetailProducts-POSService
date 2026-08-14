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
	const oldBarcode = "8901234567890"
	const newBarcode = "8901234567891"
	const branchBBarcode = "8901234567202"
	const oldCategoryID = "Fresh%20Produce%20%26%20Dairy"
	byBarcode, err := repo.GetByBarcode(ctx, oldBarcode, storeID)
	if err != nil { _ = db.Close(); t.Fatalf("initial offline barcode lookup: %v", err) }
	if byBarcode.ID != "101" || byBarcode.Name != "Fresh Milk" || byBarcode.CategoryID == nil || *byBarcode.CategoryID != oldCategoryID { _ = db.Close(); t.Fatalf("unexpected initial product: %#v", byBarcode) }
	if byBarcode.Price == nil || byBarcode.Price.AmountMinor != 6550 || byBarcode.Price.Currency != "INR" { _ = db.Close(); t.Fatalf("initial effective price mismatch: %#v", byBarcode.Price) }
	if _, err = repo.GetByBarcode(ctx, branchBBarcode, storeID); err == nil { _ = db.Close(); t.Fatal("another branch product leaked into the registered device catalog") }
	branchBMatches, err := repo.Search(ctx, "Branch B Secret", storeID, 20)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if len(branchBMatches) != 0 { _ = db.Close(); t.Fatalf("another branch product leaked into offline search: %#v", branchBMatches) }
	categories, err := repo.ListCategories(ctx)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if len(categories) != 1 || categories[0].ID != oldCategoryID { _ = db.Close(); t.Fatalf("trusted-branch category snapshot leaked or omitted categories: %#v", categories) }

	mutateCatalogState(t, centralURL, "barcode")
	more, err = puller.pullOnce(ctx)
	if err != nil { _ = db.Close(); t.Fatalf("pull replaced Central barcode: %v", err) }
	if more { _ = db.Close(); t.Fatal("unexpected page after barcode replacement") }
	if _, err = repo.GetByBarcode(ctx, oldBarcode, storeID); err == nil { _ = db.Close(); t.Fatal("stale primary barcode still resolves after Central replacement") }
	byBarcode, err = repo.GetByBarcode(ctx, newBarcode, storeID)
	if err != nil { _ = db.Close(); t.Fatalf("replacement offline barcode lookup: %v", err) }
	if byBarcode.ID != "101" || byBarcode.Name != "Fresh Milk" { _ = db.Close(); t.Fatalf("replacement barcode resolved wrong product: %#v", byBarcode) }

	mutateCatalogState(t, centralURL, "rename")
	more, err = puller.pullOnce(ctx)
	if err != nil { _ = db.Close(); t.Fatalf("pull renamed Central category: %v", err) }
	if more { _ = db.Close(); t.Fatal("unexpected page after category rename") }
	categories, err = repo.ListCategories(ctx)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if len(categories) != 1 || categories[0].ID != "New%20Dairy" || categories[0].Name != "New Dairy" { _ = db.Close(); t.Fatalf("category rename left stale/incorrect categories: %#v", categories) }
	byBarcode, err = repo.GetByBarcode(ctx, newBarcode, storeID)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if byBarcode.CategoryID == nil || *byBarcode.CategoryID != "New%20Dairy" { _ = db.Close(); t.Fatalf("product did not converge to renamed category: %#v", byBarcode.CategoryID) }

	mutateCatalogState(t, centralURL, "clear")
	more, err = puller.pullOnce(ctx)
	if err != nil { _ = db.Close(); t.Fatalf("pull cleared Central category: %v", err) }
	if more { _ = db.Close(); t.Fatal("unexpected page after category removal") }
	categories, err = repo.ListCategories(ctx)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if len(categories) != 0 { _ = db.Close(); t.Fatalf("removed Central category remained visible in POS: %#v", categories) }

	mutateCatalogState(t, centralURL, "deactivate")
	more, err = puller.pullOnce(ctx)
	if err != nil { _ = db.Close(); t.Fatalf("pull deactivated Central product: %v", err) }
	if more { _ = db.Close(); t.Fatal("unexpected page after product deactivation") }
	if _, err = repo.GetByBarcode(ctx, newBarcode, storeID); err == nil { _ = db.Close(); t.Fatal("deactivated Central product still resolves by barcode") }
	matches, err := repo.Search(ctx, "fresh milk", storeID, 20)
	if err != nil { _ = db.Close(); t.Fatal(err) }
	if len(matches) != 0 { _ = db.Close(); t.Fatalf("deactivated Central product still appears in offline search: %#v", matches) }
	deactivated, err := repo.GetProduct(ctx, "101", storeID)
	if err != nil { _ = db.Close(); t.Fatalf("load deactivated local projection: %v", err) }
	if deactivated.IsActive { _ = db.Close(); t.Fatalf("Central soft-delete did not persist as inactive POS product: %#v", deactivated) }

	var appliedBeforeRestart int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE status='applied'`).Scan(&appliedBeforeRestart); err != nil { _ = db.Close(); t.Fatal(err) }
	if err := db.Close(); err != nil { t.Fatalf("close POS database before restart: %v", err) }

	db, err = database.Open(ctx, dbPath)
	if err != nil { t.Fatalf("reopen POS database: %v", err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatalf("migrate restarted POS database: %v", err) }
	repo = catalog.NewRepository(db)
	if _, err = repo.GetByBarcode(ctx, newBarcode, storeID); err == nil { t.Fatal("deactivated product barcode returned after SQLite restart") }
	if _, err = repo.GetByBarcode(ctx, oldBarcode, storeID); err == nil { t.Fatal("stale primary barcode returned after SQLite restart") }
	if _, err = repo.GetByBarcode(ctx, branchBBarcode, storeID); err == nil { t.Fatal("another branch product appeared after SQLite restart") }
	matches, err = repo.Search(ctx, "fresh milk", storeID, 20)
	if err != nil || len(matches) != 0 { t.Fatalf("deactivated product returned after restart: matches=%#v err=%v", matches, err) }
	deactivated, err = repo.GetProduct(ctx, "101", storeID)
	if err != nil { t.Fatal(err) }
	if deactivated.IsActive || deactivated.CategoryID != nil { t.Fatalf("inactive/category-cleared state did not persist across restart: %#v", deactivated) }

	restartedPuller := New(db, inbox.New(db), centralURL, tenantID, syncToken, deviceID, 5*time.Second, time.Second)
	more, err = restartedPuller.pullOnce(ctx)
	if err != nil { t.Fatalf("replay real Central catalog changes: %v", err) }
	if more { t.Fatal("unexpected page on replay") }
	var afterReplay int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM inbox_messages WHERE status='applied'`).Scan(&afterReplay); err != nil { t.Fatal(err) }
	if afterReplay != appliedBeforeRestart { t.Fatalf("catalog replay duplicated inbox facts: before=%d after=%d", appliedBeforeRestart, afterReplay) }
}
