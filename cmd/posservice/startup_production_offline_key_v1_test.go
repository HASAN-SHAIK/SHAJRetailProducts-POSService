package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV1ProductionMissingOfflineGrantPublicKeyPreventsPOSServiceStartup(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "production-missing-offline-key.db")

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}
	address := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatalf("release local port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Env = append(os.Environ(),
		"POS_ENVIRONMENT=production",
		"POS_LISTEN_ADDRESS="+address,
		"POS_SQLITE_PATH="+dbPath,
		"POS_LOCAL_API_TOKEN=production-local-token-strong-value",
		"POS_LOCAL_TOKEN_FILE="+filepath.Join(tempDir, "pos.token"),
		"POS_BACKUP_DIRECTORY="+filepath.Join(tempDir, "backups"),
		"POS_OFFLINE_GRANT_PUBLIC_KEY=",
		"POS_CENTRAL_API_URL=",
		"POS_SYNC_TENANT_ID=",
		"POS_SYNC_TOKEN=",
		"POS_DEVICE_ID=",
		"POS_INSTALLATION_ID=",
		"POS_STORE_ID=",
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
		t.Fatalf("expected missing production offline grant public key to prevent POSService startup; output=%s", output)
	}

	logText := string(output)
	if !strings.Contains(logText, "invalid configuration") || !strings.Contains(logText, "POS_OFFLINE_GRANT_PUBLIC_KEY is required in production") {
		t.Fatalf("expected actionable production offline-key startup failure, got: %s", output)
	}

	conn, dialErr := net.DialTimeout("tcp", address, 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatalf("POSService listener became reachable despite missing production offline grant public key at %s", address)
	}

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		if err == nil {
			t.Fatalf("invalid production security configuration progressed far enough to create SQLite database %s", dbPath)
		}
		t.Fatalf("stat SQLite path after failed startup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "pos.token")); !os.IsNotExist(err) {
		if err == nil {
			t.Fatalf("invalid production security configuration progressed far enough to create local token material")
		}
		t.Fatalf("stat local token path after failed startup: %v", err)
	}

	_ = fmt.Sprint(address)
}
