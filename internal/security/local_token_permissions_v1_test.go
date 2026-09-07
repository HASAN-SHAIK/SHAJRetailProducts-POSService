package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestV1LoadOrCreateRejectsGroupWorldReadablePersistedTokenFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "pos.token")
	const token = "cycle-c-permissive-token-0123456789abcdef0123456789abcdef"

	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o644); err != nil {
		t.Fatalf("write permissive token file: %v", err)
	}
	if err := os.Chmod(tokenFile, 0o644); err != nil {
		t.Fatalf("chmod permissive token file: %v", err)
	}

	got, err := loadOrCreateToken("device-cycle-c", tokenFile)
	if err == nil {
		t.Fatalf("expected group/world-readable persisted local token file to be rejected, got token %q", got)
	}

	info, statErr := os.Stat(tokenFile)
	if statErr != nil {
		t.Fatalf("stat token file after rejection: %v", statErr)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o644 {
		t.Fatalf("token file mode mutated during rejection: got %04o want 0644", gotMode)
	}
	raw, readErr := os.ReadFile(tokenFile)
	if readErr != nil {
		t.Fatalf("read token file after rejection: %v", readErr)
	}
	if string(raw) != token+"\n" {
		t.Fatalf("token file bytes mutated: got %q", string(raw))
	}
}
