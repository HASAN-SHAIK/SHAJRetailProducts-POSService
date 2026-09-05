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

func TestV1LocalTokenInitializationFailurePreventsListenerStartup(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "token-startup.db")
	tokenPath := filepath.Join(tempDir, "token-path-is-directory")
	if err := os.Mkdir(tokenPath, 0o750); err != nil {
		t.Fatalf("create token-path directory: %v", err)
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
		"POS_LOCAL_API_TOKEN=",
		"POS_LOCAL_TOKEN_FILE="+tokenPath,
		"POS_BACKUP_DIRECTORY="+filepath.Join(tempDir, "backups"),
		"POS_CENTRAL_API_URL=",
		"POS_SYNC_TENANT_ID=",
		"POS_SYNC_TOKEN=",
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
		t.Fatalf("expected local-token initialization failure to prevent POSService startup; output=%s", output)
	}

	logText := string(output)
	if !strings.Contains(logText, "initialize local API security") || !strings.Contains(logText, "read local token file") {
		t.Fatalf("expected actionable local-token startup failure, got: %s", output)
	}

	conn, dialErr := net.DialTimeout("tcp", listenAddress, 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatalf("POSService listener became reachable despite local-token initialization failure at %s", listenAddress)
	}

	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("stat token path after failed startup: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("failed startup unexpectedly replaced token-path directory with a token file")
	}
}