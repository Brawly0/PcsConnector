# Builds small static binaries for both machines into dist/.
$ErrorActionPreference = 'Stop'
$env:Path += ";C:\Program Files\Go\bin"
New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED = '0'
$flags = '-s', '-w'

$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
go build -trimpath -ldflags "$flags" -o dist/pcsc.exe ./cmd/pcsc

$env:GOOS = 'linux'; $env:GOARCH = 'amd64'   # use arm64 for ARM laptops
go build -trimpath -ldflags "$flags" -o dist/pcsc-linux-amd64 ./cmd/pcsc

Get-ChildItem dist | Select-Object Name, @{n = 'MB'; e = { [math]::Round($_.Length / 1MB, 2) } }
