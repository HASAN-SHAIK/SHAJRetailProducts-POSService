param(
  [string]$InstallDir = "$env:ProgramFiles\SHAJRetail\POSService",
  [string]$DataDir = "$env:ProgramData\SHAJRetail\POSService",
  [string]$Binary = ".\shajretail-pos.exe"
)

$ErrorActionPreference = "Stop"
$ServiceName = "SHAJRetailPOSService"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $DataDir "backups") | Out-Null
Copy-Item $Binary (Join-Path $InstallDir "shajretail-pos.exe") -Force

$envFile = Join-Path $DataDir "pos.env"
if (-not (Test-Path $envFile)) {
@"
POS_ENVIRONMENT=production
POS_LISTEN_ADDRESS=127.0.0.1:4782
POS_SQLITE_PATH=$DataDir\shajretail-pos.db
POS_LOCAL_TOKEN_FILE=$DataDir\shajretail-pos.db.token
POS_BACKUP_DIRECTORY=$DataDir\backups
POS_OFFLINE_GRANT_PUBLIC_KEY=
POS_CENTRAL_API_URL=
POS_SYNC_TENANT_ID=
POS_SYNC_TOKEN=
POS_ALLOWED_ORIGINS=http://localhost:3000,http://127.0.0.1:3000
"@ | Set-Content -Encoding UTF8 $envFile
}

$serviceEnvironment = @()
$parsedEnvironment = @{}
Get-Content -Path $envFile | ForEach-Object {
  $line = $_.Trim()
  if (-not $line -or $line.StartsWith("#")) {
    return
  }

  $separator = $line.IndexOf("=")
  if ($separator -le 0) {
    throw "Invalid POS environment entry in $envFile. Expected KEY=VALUE."
  }

  $name = $line.Substring(0, $separator).Trim()
  $value = $line.Substring($separator + 1)
  if ($name -notmatch '^[A-Za-z_][A-Za-z0-9_]*$') {
    throw "Invalid POS environment variable name '$name' in $envFile."
  }
  $parsedEnvironment[$name] = $value
  $serviceEnvironment += "$name=$value"
}

if ($serviceEnvironment.Count -eq 0) {
  throw "No POS service environment entries were found in $envFile."
}
if ($parsedEnvironment["POS_ENVIRONMENT"] -ne "production") {
  throw "POS_ENVIRONMENT=production is required in $envFile before installing the production service."
}
if ([string]::IsNullOrWhiteSpace($parsedEnvironment["POS_OFFLINE_GRANT_PUBLIC_KEY"])) {
  throw "Configure POS_OFFLINE_GRANT_PUBLIC_KEY in $envFile before installing the production service."
}
$centralValues = @(
  $parsedEnvironment["POS_CENTRAL_API_URL"],
  $parsedEnvironment["POS_SYNC_TENANT_ID"],
  $parsedEnvironment["POS_SYNC_TOKEN"]
)
$centralConfigured = @($centralValues | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }).Count
if ($centralConfigured -ne 0 -and $centralConfigured -ne 3) {
  throw "Configure POS_CENTRAL_API_URL, POS_SYNC_TENANT_ID and POS_SYNC_TOKEN together in $envFile."
}

if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  Stop-Service $ServiceName -Force -ErrorAction SilentlyContinue
  sc.exe delete $ServiceName | Out-Null
  Start-Sleep -Seconds 1
}

$exe = Join-Path $InstallDir "shajretail-pos.exe"
sc.exe create $ServiceName binPath= "`"$exe`"" start= auto DisplayName= "SHAJRetail POS Service" | Out-Null
sc.exe description $ServiceName "Local offline-first SHAJRetail POS service" | Out-Null
sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/15000/restart/60000 | Out-Null

$serviceRegistryPath = "HKLM:\SYSTEM\CurrentControlSet\Services\$ServiceName"
New-ItemProperty -Path $serviceRegistryPath -Name Environment -PropertyType MultiString -Value $serviceEnvironment -Force | Out-Null

Start-Service $ServiceName
Write-Host "Installed $ServiceName at $exe"
Write-Host "Loaded service environment from $envFile"
