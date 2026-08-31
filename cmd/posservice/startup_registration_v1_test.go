package main

import (
	"context"
	"database/sql"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestV1PartialConfiguredRegistrationFailsClosedBeforeListener(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "partial-registration.db")

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback address: %v", err)
	}
	listenAddress := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatalf("release loopback address: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Env = append(os.Environ(),
		"POS_ENVIRONMENT=development",
		"POS_LISTEN_ADDRESS="+listenAddress,
		"POS_SQLITE_PATH="+dbPath,
		"POS_LOCAL_TOKEN_FILE="+filepath.Join(tempDir, "pos.token"),
		"POS_BACKUP_DIRECTORY="+filepath.Join(tempDir, "backups"),
		"POS_CENTRAL_API_URL=",
		"POS_SYNC_TENANT_ID=",
		"POS_SYNC_TOKEN=",
		"POS_STORE_ID=store-partial-only",
		"POS_STORE_NUMBER=",
		"POS_NO=",
		"POS_TOUCHPOINT_ID=",
		"POS_TERMINAL_ID=",
	)

	output, runErr := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("POSService did not fail closed within timeout; output=%s", output)
	}
	if runErr == nil {
		t.Fatalf("expected partial configured registration to prevent POSService startup; output=%s", output)
	}

	const wantLog = "POS_STORE_ID, POS_STORE_NUMBER, POS_NO and POS_TOUCHPOINT_ID must be configured together"
	if !strings.Contains(string(output), wantLog) {
		t.Fatalf("expected partial-registration startup error %q, got: %s", wantLog, output)
	}

	conn, dialErr := net.DialTimeout("tcp", listenAddress, 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatalf("POSService listener became reachable despite incomplete configured registration at %s", listenAddress)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open startup SQLite for persisted-state verification: %v", err)
	}
	defer db.Close()

	var storeID, storeNumber, posNo, touchpointID sql.NullString
	if err := db.QueryRowContext(context.Background(), `
		SELECT store_id, store_number, pos_no, touchpoint_id
		FROM device_identity
		LIMIT 1
	`).Scan(&storeID, &storeNumber, &posNo, &touchpointID); err != nil {
		t.Fatalf("read persisted device identity after fail-closed startup: %v", err)
	}
	if storeID.Valid || storeNumber.Valid || posNo.Valid || touchpointID.Valid {
		t.Fatalf("partial configured registration leaked into SQLite: store_id=%q store_number=%q pos_no=%q touchpoint_id=%q",
			storeID.String, storeNumber.String, posNo.String, touchpointID.String)
	}
}
