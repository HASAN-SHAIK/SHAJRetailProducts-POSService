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

func TestRealCentralImportedProductConvergenceE2E(t *testing.T) {
	centralURL := os.Getenv("POS_E2E_CENTRAL_URL")
	if centralURL == "" { t.Skip("POS_E2E_CENTRAL_URL is required") }
	tenantID, token, deviceID := os.Getenv("POS_E2E_TENANT_ID"), os.Getenv("POS_E2E_SYNC_TOKEN"), os.Getenv("POS_E2E_DEVICE_ID")
	if tenantID == "" || token == "" || deviceID == "" { t.Fatal("catalog E2E env is incomplete") }

	req, err := http.NewRequest(http.MethodPost, centralURL+"/e2e/import", nil)
	if err != nil { t.Fatal(err) }
	resp, err := http.DefaultClient.Do(req)
	if err != nil { t.Fatal(err) }
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated { t.Fatalf("Central import returned %d", resp.StatusCode) }

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "import-catalog-e2e.db")
	db, err := database.Open(ctx, dbPath)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }
	puller := New(db, inbox.New(db), centralURL, tenantID, token, deviceID, 5*time.Second, time.Second)
	for {
		more, err := puller.pullOnce(ctx)
		if err != nil { t.Fatalf("pull imported Central catalog: %v", err) }
		if !more { break }
	}

	repo := catalog.NewRepository(db)
	const storeID = "11111111-1111-1111-1111-111111111111"
	product, err := repo.GetByBarcode(ctx, "8901234567404", storeID)
	if err != nil { t.Fatalf("offline imported barcode lookup: %v", err) }
	if product.Name != "Imported Catalog Milk" { t.Fatalf("unexpected imported product: %#v", product) }
	if product.Price == nil || product.Price.AmountMinor != 5550 || product.Price.Currency != "INR" {
		t.Fatalf("imported effective price mismatch: %#v", product.Price)
	}
	matches, err := repo.Search(ctx, "Imported Catalog Milk", storeID, 20)
	if err != nil || len(matches) != 1 { t.Fatalf("offline imported name lookup failed: matches=%#v err=%v", matches, err) }
}
