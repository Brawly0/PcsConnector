# PcsConnector

Use an Ubuntu laptop as an extended screen + shared input/clipboard/files for a Windows PC.

- What it is: Go tool (`cmd/pcsc`), single static binary per OS, chosen for low RAM/CPU. See README.md, docs/ARCHITECTURE.md.
- How to run it: `./build.ps1` -> dist/. Double-click PcsConnector.exe (UI at 127.0.0.1:24801; code in cmd/pcsc/ui.go + ui.html). CLI: `pcsc host` / `pcsc join <ip> --token T`. Test: `go test ./...` (Go is at C:\Program Files\Go\bin, may need adding to PATH).
- Conventions: stdlib only (+ x/crypto/scrypt). Windows clipboard via user32 syscalls, Linux via xclip/wl-clipboard. `PCSC_HOME` overrides data dir. Prefer Deskflow (Input Leap is archived) for input until native input exists.
