package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestV1LoadOrCreateRejectsDirectoryTokenPath(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "pos.token")
	if err := os.Mkdir(tokenPath, 0o700); err != nil {
		t.Fatalf("create token directory fixture: %v", err)
	}

	if _, err := loadOrCreateToken("device-cycle-c", tokenPath); err == nil {
		t.Fatal("expected directory local token path to be rejected")
	}

	info, err := os.Lstat(tokenPath)
	if err != nil {
		t.Fatalf("lstat token directory after rejection: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("token path mutated; mode=%v", info.Mode())
	}
}
