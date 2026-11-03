# Builds small static binaries for both machines into dist/.
$ErrorActionPreference = 'Stop'
$env:Path += ";C:\Program Files\Go\bin"
Remove-Item -Recurse -Force dist -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED = '0'

# Windows: PcsConnector.exe = app window (no console). pcsc.exe = same program with a console for CLI use.
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
go build -trimpath -ldflags "-s -w -H=windowsgui" -o dist/PcsConnector.exe ./cmd/pcsc
go build -trimpath -ldflags "-s -w" -o dist/pcsc.exe ./cmd/pcsc

# Ubuntu bundle: pcsc + install.sh  ->  dist/pcsconnector-ubuntu.tar.gz   (use GOARCH=arm64 for ARM laptops)
$env:GOOS = 'linux'; $env:GOARCH = 'amd64'
$b = 'dist/pcsconnector-ubuntu'
New-Item -ItemType Directory -Force $b | Out-Null
go build -trimpath -ldflags "-s -w" -o "$b/pcsc" ./cmd/pcsc
Copy-Item install/install.sh $b/
tar -czf dist/pcsconnector-ubuntu.tar.gz -C dist pcsconnector-ubuntu

Get-ChildItem dist -File | Select-Object Name, @{n = 'MB'; e = { [math]::Round($_.Length / 1MB, 2) } }
