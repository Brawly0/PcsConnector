# Architecture

```
 Windows PC (host)                          Ubuntu laptop (client)
 ┌───────────────────────┐   TCP 24800      ┌──────────────────────┐
 │ pcsc host             │◄════════════════►│ pcsc join            │
 │  clipboard · files    │  AES-GCM frames  │  clipboard · files   │
 ├───────────────────────┤                  ├──────────────────────┤
 │ virtual display + GDI ── JPEG frames ───►│ app window (fullscr) │   (Phase 3)
 │ input capture ◄──────── input events ────│ input injection      │   (Phase 2/3)
 └───────────────────────┘                  └──────────────────────┘
```

## Current code (Phase 1, Go)
- `cmd/pcsc/protocol.go` – length-prefixed frames, AES-256-GCM, key = scrypt(token).
- `cmd/pcsc/app.go` – connection lifecycle: serve / dial (auto-reconnect), message dispatch, newest connection wins.
- `cmd/pcsc/clipboard*.go` – Windows: user32 syscalls + `GetClipboardSequenceNumber` (no polling cost); Linux: xclip / wl-clipboard polling.
- `cmd/pcsc/files.go` – Outbox watcher (waits for stable size) -> chunked send -> Sent; receiver writes to Inbox (basename only, never overwrites).

Why Go: single static binary (~3.5 MB), ~9 MB RAM idle, no runtime on Ubuntu, cross-compiles Linux from Windows, and can call Win32 / uinput directly for the input phase.

## Roadmap
1. **Phase 1 (done):** secure channel, clipboard text, folder-based file transfer.
2. **Phase 2 – input:** reuse Deskflow first (Input Leap is archived). Native replacement: capture mouse/keys on Windows (low-level hooks), send over the existing channel when the cursor crosses the screen edge, inject on Ubuntu via `uinput`/XTest. Needs a low-level keyboard/mouse hooks on Windows (`SetWindowsHookEx` via syscall) and `/dev/uinput` on Linux – both doable in Go without cgo.
3. **Phase 3 – extended screen:** built-in GDI capture of the virtual monitor, JPEG frames over the encrypted channel (frame type 2), bundled driver installed by one click. Next: resolution negotiation, hardware encoding. Optionally forward clicks made on the laptop's stream back to the virtual monitor.
4. **Phase 4 – polish:** richer clipboard (images, files), drag-and-drop files, TLS/pairing instead of a pasted token, tray UI, installers.

## Known limits
- Text-only clipboard. Large clipboards (>4 MB) are dropped by the frame limit.
- No replay protection / forward secrecy.
- The virtual display driver is installed with one UAC prompt; it is a third-party signed driver (Virtual-Display-Driver).
