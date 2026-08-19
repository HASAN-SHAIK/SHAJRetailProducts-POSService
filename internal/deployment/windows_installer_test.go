package deployment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWindowsInstaller(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join("..", "..", "packaging", "windows", "install-service.ps1")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read Windows installer: %v", err)
	}
	return string(content)
}

func TestWindowsInstallerLoadsPOSServiceEnvironment(t *testing.T) {
	script := readWindowsInstaller(t)

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

func TestWindowsInstallerPreservesDurablePOSStateOnReinstall(t *testing.T) {
	script := readWindowsInstaller(t)

	if !strings.Contains(script, `if (-not (Test-Path $envFile))`) {
		t.Fatal("reinstall must preserve an existing POS environment file")
	}
	for _, durablePath := range []string{
		`POS_SQLITE_PATH=$DataDir\shajretail-pos.db`,
		`POS_LOCAL_TOKEN_FILE=$DataDir\shajretail-pos.db.token`,
		`POS_BACKUP_DIRECTORY=$DataDir\backups`,
	} {
		if !strings.Contains(script, durablePath) {
			t.Fatalf("installer must keep durable state under DataDir: %q", durablePath)
		}
	}
	for _, destructive := range []string{`Remove-Item $DataDir`, `Remove-Item -Recurse $DataDir`, `Clear-Content $envFile`} {
		if strings.Contains(script, destructive) {
			t.Fatalf("reinstall must not destroy durable POS state: %q", destructive)
		}
	}
}
