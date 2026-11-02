# Architecture

```
 Windows PC (host)                          Ubuntu laptop (client)
 ┌───────────────────────┐   TCP 24800      ┌──────────────────────┐
 │ pcsc host             │◄════════════════►│ pcsc join            │
 │  clipboard · files    │  AES-GCM frames  │  clipboard · files   │
 ├───────────────────────┤                  ├──────────────────────┤
 │ virtual display + ffmpeg ── UDP 5000 ───►│ ffplay (fullscreen)  │   (Phase 3)
 │ input capture ◄──────── input events ────│ input injection      │   (Phase 2/3)
 └───────────────────────┘                  └──────────────────────┘
```

## Current code (Phase 1)
- `src/protocol.js` – length-prefixed frames, AES-256-GCM, key = scrypt(token). Frame types: JSON control, binary file chunk.
- `src/session.js` – one connection; `send`, `sendChunk` (with backpressure), events.
- `src/clipboard.js` – text clipboard via PowerShell / xclip / wl-clipboard; polling with echo suppression.
- `src/files.js` – Outbox watcher (waits for stable size) → chunked send → Sent; `Receiver` writes to Inbox (basename only, never overwrites).
- `src/cli.js` – `token`, `host`, `join`, `doctor`. Client auto-reconnects every 3 s. Newest host connection wins.

Zero npm dependencies on purpose: keeps it trivial to run on both OSes.

## Roadmap
1. **Phase 1 (done):** secure channel, clipboard text, folder-based file transfer.
2. **Phase 2 – input:** reuse Deskflow/Input Leap first. Native replacement: capture mouse/keys on Windows (low-level hooks), send over the existing channel when the cursor crosses the screen edge, inject on Ubuntu via `uinput`/XTest. Needs a small native helper (Rust or C#) – Node alone can't do global hooks.
3. **Phase 3 – extended screen:** app-managed ffmpeg capture/stream of the virtual monitor, auto-start/stop with the session, resolution negotiation, hardware encoding. Optionally forward clicks made on the laptop's stream back to the virtual monitor.
4. **Phase 4 – polish:** richer clipboard (images, files), drag-and-drop files, TLS/pairing instead of a pasted token, tray UI, installers.

## Known limits
- Text-only clipboard. Large clipboards (>4 MB) are dropped by the frame limit.
- No replay protection / forward secrecy.
- The virtual display driver and its install are manual (needs admin + driver signing considerations).
