# PcsConnector

Use an Ubuntu laptop as an extended screen + shared input/clipboard/files for a Windows PC.

- What it is: Go tool (`cmd/pcsc`), single static binary per OS, chosen for low RAM/CPU. See README.md, docs/ARCHITECTURE.md.
- How to run it: `./build.ps1` -> dist/. `pcsc host` (Windows) / `pcsc join <ip> --token T` (Ubuntu). Test: `go test ./...` (Go is at C:\Program Files\Go\bin, may need adding to PATH).
- Conventions: stdlib only (+ x/crypto/scrypt). Windows clipboard via user32 syscalls, Linux via xclip/wl-clipboard. `PCSC_HOME` overrides data dir. Prefer Deskflow (Input Leap is archived) for input until native input exists.
