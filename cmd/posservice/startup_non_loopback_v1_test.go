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

func TestV1NonLoopbackListenAddressPreventsPOSServiceStartup(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "non-loopback-startup.db")

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatalf("release local port: %v", err)
	}

	nonLoopbackAddress := net.JoinHostPort("0.0.0.0", fmt.Sprint(port))
	loopbackProbeAddress := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Env = append(os.Environ(),
		"POS_ENVIRONMENT=development",
		"POS_LISTEN_ADDRESS="+nonLoopbackAddress,
		"POS_SQLITE_PATH="+dbPath,
		"POS_LOCAL_API_TOKEN=test-local-token",
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
		t.Fatalf("expected non-loopback bind configuration to prevent POSService startup; output=%s", output)
	}

	logText := string(output)
	if !strings.Contains(logText, "invalid configuration") || !strings.Contains(logText, "POS_LISTEN_ADDRESS must bind to loopback only") {
		t.Fatalf("expected actionable non-loopback startup failure, got: %s", output)
	}

	conn, dialErr := net.DialTimeout("tcp", loopbackProbeAddress, 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatalf("POSService listener became reachable despite rejected non-loopback address at %s", loopbackProbeAddress)
	}

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		if err == nil {
			t.Fatalf("invalid network binding progressed far enough to create SQLite database %s", dbPath)
		}
		t.Fatalf("stat SQLite path after failed startup: %v", err)
	}
}
