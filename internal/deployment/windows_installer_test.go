package deployment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsInstallerLoadsPOSServiceEnvironment(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "packaging", "windows", "install-service.ps1")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read Windows installer: %v", err)
	}
	script := string(content)

	required := []string{
		`Get-Content -Path $envFile`,
		`Expected KEY=VALUE`,
		`^[A-Za-z_][A-Za-z0-9_]*$`,
		`$serviceRegistryPath = "HKLM:\SYSTEM\CurrentControlSet\Services\$ServiceName"`,
		`-Name Environment -PropertyType MultiString -Value $serviceEnvironment`,
		`Loaded service environment from $envFile`,
	}
	for _, want := range required {
		if !strings.Contains(script, want) {
			t.Fatalf("Windows installer must contain %q", want)
		}
	}

	registryIndex := strings.Index(script, `-Name Environment -PropertyType MultiString -Value $serviceEnvironment`)
	startIndex := strings.Index(script, `Start-Service $ServiceName`)
	if registryIndex < 0 || startIndex < 0 || registryIndex > startIndex {
		t.Fatal("service environment must be persisted before the Windows service starts")
	}
}
