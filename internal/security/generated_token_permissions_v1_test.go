package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestV1GeneratedLocalTokenIsOwnerOnlyAndReloadable(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "security", "pos.token")

	if _, err := LoadOrCreate("device-cycle-c-permissions", "", tokenFile, nil); err != nil {
		t.Fatalf("first LoadOrCreate failed: %v", err)
	}

	info, err := os.Stat(tokenFile)
	if err != nil {
		t.Fatalf("stat generated token file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected generated token file mode 0600, got %04o", got)
	}

	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read generated token file: %v", err)
	}
	if len(raw) < 33 {
		t.Fatalf("expected non-empty persisted token, got %d bytes", len(raw))
	}

	if _, err := LoadOrCreate("device-cycle-c-permissions", "", tokenFile, nil); err != nil {
		t.Fatalf("reload of generated token failed: %v", err)
	}

	after, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("reread generated token file: %v", err)
	}
	if string(after) != string(raw) {
		t.Fatal("generated token file changed during reload")
	}
}
