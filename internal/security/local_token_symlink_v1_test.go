package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestV1LoadOrCreateRejectsSymlinkedLocalTokenFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "external-secret")
	tokenFile := filepath.Join(dir, "pos.token")
	const token = "cycle-c-symlink-token-0123456789abcdef0123456789abcdef"

	if err := os.WriteFile(target, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write target token: %v", err)
	}
	if err := os.Symlink(target, tokenFile); err != nil {
		t.Fatalf("create token symlink: %v", err)
	}

	got, err := loadOrCreateToken("device-cycle-c", tokenFile)
	if err == nil {
		t.Fatalf("expected symlinked local token file to be rejected, got token %q", got)
	}

	info, statErr := os.Lstat(tokenFile)
	if statErr != nil {
		t.Fatalf("lstat token file: %v", statErr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("token path is no longer a symlink: mode=%v", info.Mode())
	}
	raw, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read target token after rejection: %v", readErr)
	}
	if string(raw) != token+"\n" {
		t.Fatalf("symlink target mutated: got %q", string(raw))
	}
}
