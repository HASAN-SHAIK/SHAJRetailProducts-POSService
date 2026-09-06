package security

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV1LoadOrCreateRejectsShortPersistedLocalToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "pos.token")
	original := []byte("short-cycle-c-token\n")
	if err := os.WriteFile(tokenFile, original, 0o600); err != nil {
		t.Fatalf("write invalid token file: %v", err)
	}

	if _, err := LoadOrCreate("device-cycle-c", "", tokenFile, []string{"http://127.0.0.1:5173"}); err == nil {
		t.Fatal("expected invalid persisted local token to be rejected")
	} else if !strings.Contains(err.Error(), "local API token file contains an invalid token") {
		t.Fatalf("unexpected invalid-token error: %v", err)
	}

	after, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read token file after rejection: %v", err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("invalid persisted token was unexpectedly modified: got %q want %q", after, original)
	}
}
