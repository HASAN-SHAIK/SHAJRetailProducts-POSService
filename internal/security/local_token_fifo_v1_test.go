package security

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestV1LoadOrCreateRejectsNamedPipeTokenFileWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "pos.token")
	if err := syscall.Mkfifo(tokenFile, 0o600); err != nil {
		t.Fatalf("create token FIFO: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := loadOrCreateToken("device-cycle-c", tokenFile)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected named-pipe local token path to be rejected")
		}
	case <-time.After(1200 * time.Millisecond):
		t.Fatal("loadOrCreateToken blocked on named-pipe local token path instead of failing closed")
	}

	info, err := os.Lstat(tokenFile)
	if err != nil {
		t.Fatalf("lstat token FIFO after rejection: %v", err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("token path mutated; mode=%v", info.Mode())
	}
}
