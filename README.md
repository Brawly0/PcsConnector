# PcsConnector

Use an **Ubuntu laptop as an extended screen for a Windows PC**, with shared mouse/keyboard, clipboard and file transfer.

| Feature | How | Status |
|---|---|---|
| Clipboard sync (text) | `pcsc` (this repo), encrypted TCP | ✅ working |
| File transfer | `pcsc` – drop into `Outbox`, appears in peer's `Inbox` | ✅ working |
| Mouse + keyboard sharing | Deskflow / Input Leap (Phase 2 → native) | 📄 documented, not built |
| Extended screen | Virtual display on Windows + ffmpeg stream to Ubuntu | 📄 documented, not built |

See [docs/SETUP.md](docs/SETUP.md) for step-by-step setup and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design and roadmap.

## Quick start (clipboard + files)

Requires Node.js 18+ on both machines. Windows needs nothing else; Ubuntu needs `xclip` (X11) or `wl-clipboard` (Wayland).

```bash
git clone https://github.com/Brawly0/PcsConnector && cd PcsConnector

# Windows PC
node bin/pcsc.js token          # prints a shared secret
node bin/pcsc.js host

# Ubuntu laptop
node bin/pcsc.js join <WINDOWS_IP> --token <SECRET>
```

Copy text on one machine and paste on the other. Drop files into `~/PcsConnector/Outbox` to send them; received files land in `~/PcsConnector/Inbox`.

`node bin/pcsc.js doctor` checks that the needed tools are installed. `npm test` runs a loopback file-transfer test.

## Security

Every frame is AES-256-GCM encrypted with a key derived from the shared token; a wrong token cannot connect. There is no replay protection or forward secrecy yet — use on a trusted LAN or over a VPN/Tailscale, and don't expose port 24800 to the internet.

## License

MIT
