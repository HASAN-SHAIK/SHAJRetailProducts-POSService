package server

import (
	"context"
	"sync"
	"testing"
)

func TestManagerApprovalConcurrentConsumeSucceedsExactlyOnce(t *testing.T) {
	ctx := context.Background()
	db := openSensitiveApprovalDB(t)
	token := "concurrent-single-use-token"
	seedSensitiveApproval(t, db, token, "cashier-1", permissionPOSRefund)

	s := &Server{db: db}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	consume := func() {
		defer wg.Done()
		<-start
		_, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund)
		results <- err
	}
	go consume()
	go consume()
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent approval consumption successes=%d failures=%d want=1/1", successes, failures)
	}

	var consumedAtCount int
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pos_manager_approvals
		WHERE cashier_user_id=? AND permission=? AND consumed_at IS NOT NULL`,
		"cashier-1", permissionPOSRefund,
	).Scan(&consumedAtCount); err != nil {
		t.Fatal(err)
	}
	if consumedAtCount != 1 {
		t.Fatalf("consumed approvals=%d want=1", consumedAtCount)
	}

	if _, err := s.consumeManagerApproval(ctx, token, "cashier-1", permissionPOSRefund); err == nil {
		t.Fatal("approval remained consumable after concurrent race")
	}
}
