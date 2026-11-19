# Builds small static binaries for every platform into dist/ (these are also the GitHub release files).
#   dist/PcsConnector.exe        Windows app (double-click; no console window)
#   dist/pcsc.exe                the same program with a console, for the command line
#   dist/pcsc-linux-amd64|arm64  Linux app (install.sh downloads these from the release)
#   dist/install.sh              Linux installer
#   dist/pcsconnector-ubuntu.tar.gz  offline Linux bundle: pcsc + install.sh
# Usage: ./build.ps1 [-Version v1.2.0]   (default version: date + git commit)
param([string]$Version = '')
$ErrorActionPreference = 'Stop'
$env:Path += ";C:\Program Files\Go\bin"
Remove-Item -Recurse -Force dist -ErrorAction SilentlyContinue
if (Test-Path dist) { throw 'dist/ is locked: close PcsConnector first (Get-Process PcsConnector | Stop-Process)' }
New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED = '0'
if (-not $Version) {
  # date + git commit (+dirty), shown by `pcsc version`, `doctor` and the app window
  $rev = (git rev-parse --short HEAD 2>$null); if (git status --porcelain -- cmd install 2>$null) { $rev += '+dirty' }
  $Version = (Get-Date -Format 'yyyy.MM.dd') + '-' + $rev
}
$lf = "-s -w -X main.version=$Version"

$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
go build -trimpath -ldflags "$lf -H=windowsgui" -o dist/PcsConnector.exe ./cmd/pcsc
go build -trimpath -ldflags $lf -o dist/pcsc.exe ./cmd/pcsc

$env:GOOS = 'linux'
foreach ($arch in 'amd64', 'arm64') {
  $env:GOARCH = $arch
  go build -trimpath -ldflags $lf -o "dist/pcsc-linux-$arch" ./cmd/pcsc
}
Copy-Item install/install.sh dist/

# offline bundle (amd64): copy the folder to the laptop and run `bash pcsconnector-ubuntu/install.sh`
$b = 'dist/pcsconnector-ubuntu'
New-Item -ItemType Directory -Force $b | Out-Null
Copy-Item dist/pcsc-linux-amd64 "$b/pcsc"
Copy-Item install/install.sh $b/
tar -czf dist/pcsconnector-ubuntu.tar.gz -C dist pcsconnector-ubuntu
Remove-Item -Recurse -Force $b

Write-Host "PcsConnector $Version"
Get-ChildItem dist -File | Select-Object Name, @{n = 'MB'; e = { [math]::Round($_.Length / 1MB, 2) } }
