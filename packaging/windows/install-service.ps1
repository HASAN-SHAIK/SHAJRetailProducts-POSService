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
POS_LISTEN_ADDRESS=127.0.0.1:4782
POS_SQLITE_PATH=$DataDir\shajretail-pos.db
POS_LOCAL_TOKEN_FILE=$DataDir\shajretail-pos.db.token
POS_BACKUP_DIRECTORY=$DataDir\backups
"@ | Set-Content -Encoding UTF8 $envFile
}

$serviceEnvironment = @()
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
  $serviceEnvironment += "$name=$value"
}

if ($serviceEnvironment.Count -eq 0) {
  throw "No POS service environment entries were found in $envFile."
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
