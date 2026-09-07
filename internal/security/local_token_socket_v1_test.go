package security

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestV1LoadOrCreateRejectsUnixSocketTokenFile(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "pos.token")
	addr := &net.UnixAddr{Name: tokenFile, Net: "unix"}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("create token unix socket: %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatalf("close token unix socket fixture: %v", err)
	}

	if _, err := loadOrCreateToken("device-cycle-c", tokenFile); err == nil {
		t.Fatal("expected unix-socket local token path to be rejected")
	}

	info, err := os.Lstat(tokenFile)
	if err != nil {
		t.Fatalf("lstat token socket after rejection: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("token path mutated; mode=%v", info.Mode())
	}
}
