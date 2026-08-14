package inbox

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func snapshotMessage(t *testing.T, id string, version int, categories []categorySnapshotEntry) Message {
	t.Helper()
	payload, err := json.Marshal(categorySnapshotPayload{Categories: categories, Version: version})
	if err != nil { t.Fatal(err) }
	return Message{ID: id, Type: "catalog.categories.snapshot", SchemaVersion: 1, Source: "central", Payload: payload}
}

func categoryMessage(t *testing.T, id, categoryID, name string, version int) Message {
	t.Helper()
	payload, err := json.Marshal(categoryPayload{ID: categoryID, Name: name, IsActive: true, Version: version})
	if err != nil { t.Fatal(err) }
	return Message{ID: id, Type: "catalog.category.upsert", SchemaVersion: 1, Source: "central", Payload: payload}
}

func TestV1CategorySnapshotDeactivatesMissingCategoriesAndPreservesNewerFacts(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "catalog-snapshot.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }
	service := New(db)

	if err := service.Apply(ctx, categoryMessage(t, "category-a", "A", "Old Category", 100)); err != nil { t.Fatal(err) }
	if err := service.Apply(ctx, snapshotMessage(t, "snapshot-200", 200, []categorySnapshotEntry{{ID: "B", Name: "New Category"}})); err != nil { t.Fatal(err) }

	var activeA, activeB, versionA, versionB int
	if err := db.SQL().QueryRow(`SELECT is_active,version FROM catalog_categories WHERE id='A'`).Scan(&activeA,&versionA); err != nil { t.Fatal(err) }
	if err := db.SQL().QueryRow(`SELECT is_active,version FROM catalog_categories WHERE id='B'`).Scan(&activeB,&versionB); err != nil { t.Fatal(err) }
	if activeA != 0 || versionA != 200 { t.Fatalf("missing category was not deactivated at snapshot version: active=%d version=%d", activeA, versionA) }
	if activeB != 1 || versionB != 200 { t.Fatalf("current category was not activated at snapshot version: active=%d version=%d", activeB, versionB) }

	if err := service.Apply(ctx, categoryMessage(t, "category-c", "C", "Newer Category", 300)); err != nil { t.Fatal(err) }
	if err := service.Apply(ctx, snapshotMessage(t, "snapshot-old", 250, []categorySnapshotEntry{{ID: "B", Name: "New Category"}})); err != nil { t.Fatal(err) }

	var activeC, versionC int
	if err := db.SQL().QueryRow(`SELECT is_active,version FROM catalog_categories WHERE id='C'`).Scan(&activeC,&versionC); err != nil { t.Fatal(err) }
	if activeC != 1 || versionC != 300 { t.Fatalf("older snapshot overrode newer category fact: active=%d version=%d", activeC, versionC) }
}

func TestV1CategorySnapshotReplayIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "catalog-snapshot-replay.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(ctx); err != nil { t.Fatal(err) }
	service := New(db)
	message := snapshotMessage(t, "snapshot-100", 100, []categorySnapshotEntry{{ID: "A", Name: "Category A"}})
	if err := service.Apply(ctx, message); err != nil { t.Fatal(err) }
	if err := service.Apply(ctx, message); err != nil { t.Fatal(err) }

	var count, attempts int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM catalog_categories WHERE id='A' AND is_active=1`).Scan(&count); err != nil { t.Fatal(err) }
	if count != 1 { t.Fatalf("expected one active category after replay, got %d", count) }
	if err := db.SQL().QueryRow(`SELECT attempt_count FROM inbox_messages WHERE message_id='snapshot-100'`).Scan(&attempts); err != nil { t.Fatal(err) }
	if attempts != 1 { t.Fatalf("already-applied replay should not increment attempts, got %d", attempts) }
}
