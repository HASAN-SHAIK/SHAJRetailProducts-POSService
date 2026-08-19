package deployment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxServiceUsesExplicitConfigAndHardenedStatePaths(t *testing.T) {
	servicePath := filepath.Join("..", "..", "packaging", "linux", "shajretail-pos.service")
	content, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("read Linux service: %v", err)
	}
	service := string(content)

	required := []string{
		`User=shajretail-pos`,
		`Group=shajretail-pos`,
		`EnvironmentFile=-/etc/shajretail-pos/pos.env`,
		`ExecStart=/opt/shajretail-pos/shajretail-pos`,
		`Restart=on-failure`,
		`NoNewPrivileges=true`,
		`PrivateTmp=true`,
		`ProtectSystem=strict`,
		`ProtectHome=true`,
		`ReadWritePaths=/var/lib/shajretail-pos /var/backups/shajretail-pos`,
		`UMask=0077`,
	}
	for _, want := range required {
		if !strings.Contains(service, want) {
			t.Fatalf("Linux service must contain %q", want)
		}
	}

	if strings.Contains(service, `Environment=POS_SYNC_TOKEN=`) || strings.Contains(service, `Environment=POS_LOCAL_API_TOKEN=`) {
		t.Fatal("Linux service unit must not embed POS secret values")
	}
}
