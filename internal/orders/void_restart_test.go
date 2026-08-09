package orders

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

func TestVoidSurvivesRestartAndReplayRemainsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pos.db")

	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate before void: %v", err)
	}

	insertVoidTestOrder(t, db, ctx, "ord-void-restart", "confirmed", nil)
	svc := &Service{db: db}
	voided, err := svc.VoidWith(ctx, "ord-void-restart", "manager-1", "customer cancelled before completion")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if voided.Status != "cancelled" || voided.Version != 2 {
		db.Close()
		t.Fatalf("voided order=%+v", voided)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("migrate after restart: %v", err)
	}

	var status, approver, reason string
	var version int
	if err := reopened.SQL().QueryRowContext(ctx, `
		SELECT status,version,approved_by_user_id,approval_reason
		FROM sales_orders WHERE id=?`, "ord-void-restart").Scan(&status, &version, &approver, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || version != 2 || approver != "manager-1" || reason != "customer cancelled before completion" {
		t.Fatalf("restart facts status=%s version=%d approver=%s reason=%s", status, version, approver, reason)
	}

	restartedService := &Service{db: reopened}
	if _, err := restartedService.VoidWith(ctx, "ord-void-restart", "manager-1", "customer cancelled before completion"); !errors.Is(err, ErrAlreadyVoided) {
		t.Fatalf("replayed void error=%v want=%v", err, ErrAlreadyVoided)
	}

	var versionAfterReplay int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT version FROM sales_orders WHERE id=?`, "ord-void-restart").Scan(&versionAfterReplay); err != nil {
		t.Fatal(err)
	}
	if versionAfterReplay != 2 {
		t.Fatalf("replayed void changed version=%d want=2", versionAfterReplay)
	}
}
