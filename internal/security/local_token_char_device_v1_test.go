package security

import (
	"os"
	"testing"
)

func TestV1LoadOrCreateRejectsCharacterDeviceTokenPath(t *testing.T) {
	const tokenPath = "/dev/null"
	info, err := os.Lstat(tokenPath)
	if err != nil {
		t.Skipf("character-device fixture unavailable: %v", err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device; mode=%v", tokenPath, info.Mode())
	}

	if _, err := loadOrCreateToken("device-cycle-c", tokenPath); err == nil {
		t.Fatal("expected character-device local token path to be rejected")
	}

	after, err := os.Lstat(tokenPath)
	if err != nil {
		t.Fatalf("lstat character device after rejection: %v", err)
	}
	if after.Mode()&os.ModeCharDevice == 0 {
		t.Fatalf("character-device token path mutated; mode=%v", after.Mode())
	}
}
