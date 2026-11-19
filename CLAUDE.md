# PcsConnector

Use an Ubuntu laptop as an extended screen + shared input/clipboard/files for a Windows PC.

- What it is: Go tool (`cmd/pcsc`), single static binary per OS, chosen for low RAM/CPU. See README.md, docs/ARCHITECTURE.md.
- How to run it: `./build.ps1` -> dist/. Double-click PcsConnector.exe (UI at 127.0.0.1:24801; code in cmd/pcsc/ui.go + ui.html). CLI: `pcsc host` / `pcsc join <ip> --token T`. Test: `go test ./...` (Go is at C:\Program Files\Go\bin, may need adding to PATH).
- Conventions: stdlib only (+ x/crypto/scrypt). Windows clipboard via user32 syscalls, Linux via xclip/wl-clipboard. `PCSC_HOME` overrides data dir. Mouse/keyboard sharing is built in (kvm*.go, fastpath.go); pairing is code-only via discover.go.
- Releases: `./build.ps1 -Version vX.Y.Z` -> upload dist/ files (PcsConnector.exe, pcsc.exe, pcsc-linux-amd64, pcsc-linux-arm64, install.sh, pcsconnector-ubuntu.tar.gz). install.sh downloads `pcsc-linux-<arch>` from the latest release.
