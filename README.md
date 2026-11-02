# PcsConnector

Use an **Ubuntu laptop as an extended screen for a Windows PC**, with shared mouse/keyboard, clipboard and file transfer.

Written in **Go**: one ~3.5 MB static binary per OS, ~9 MB RAM idle, nothing to install at runtime.

| Feature | How | Status |
|---|---|---|
| Clipboard sync (text) | `pcsc`, encrypted TCP; event-driven on Windows | ✅ working (Windows side verified) |
| File transfer | `pcsc` – drop into `Outbox`, appears in peer's `Inbox` | ✅ working (loopback tested) |
| Mouse + keyboard sharing | [Deskflow](https://github.com/deskflow/deskflow) now; native later | 📄 documented, not built |
| Extended screen | Virtual display on Windows + ffmpeg stream to Ubuntu | 📄 documented, not built |

> Input Leap is archived (last release June 2025) – use Deskflow, which is actively maintained.

Docs: [setup](docs/SETUP.md) · [architecture & roadmap](docs/ARCHITECTURE.md)

## Quick start

Build (needs Go; produces both binaries in `dist/`):

```powershell
./build.ps1
```

Copy `dist/pcsc-linux-amd64` to the laptop (`chmod +x`). On Ubuntu install `xclip` (X11) or `wl-clipboard` (Wayland).

```text
Windows:  pcsc.exe token          # prints a shared secret
          pcsc.exe host
Ubuntu:   ./pcsc-linux-amd64 join <WINDOWS_IP> --token <SECRET>
```

Copy text on one machine, paste on the other. Drop files into `~/PcsConnector/Outbox` to send; they arrive in the other machine's `Inbox`. `pcsc doctor` checks needed tools. `go test ./...` runs the tests.

## Security

Frames are AES-256-GCM encrypted with a key derived (scrypt) from the shared token; a wrong token can't connect. No replay protection or forward secrecy yet – use on a trusted LAN or VPN, never expose port 24800 to the internet.

## License
MIT
