package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV1CorruptSQLiteFailsClosedBeforeListener(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "corrupt-pos.db")
	if err := os.WriteFile(dbPath, []byte("this is deliberately not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt sqlite fixture: %v", err)
	}

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
	)

	output, runErr := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("POSService did not fail closed within timeout; output=%s", output)
	}
	if runErr == nil {
		t.Fatalf("expected corrupt SQLite to prevent POSService startup; output=%s", output)
	}

	logOutput := string(output)
	if !strings.Contains(logOutput, "open local database") &&
		!strings.Contains(logOutput, "apply local database migrations") &&
		!strings.Contains(logOutput, "local database integrity check failed") {
		t.Fatalf("expected actionable SQLite startup failure log, got: %s", logOutput)
	}

	conn, dialErr := net.DialTimeout("tcp", listenAddress, 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatalf("POSService listener became reachable despite corrupt SQLite at %s", listenAddress)
	}
}
