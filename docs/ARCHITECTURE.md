# Architecture

```
 Windows PC (host)                                   Ubuntu laptop (client)
 ┌──────────────────────────┐   TCP 24800 (AES-GCM)   ┌──────────────────────────┐
 │ pcsc (serve)             │◄═══════════════════════►│ pcsc (dial)              │
 │  clipboard · files       │                         │  clipboard · files       │
 │  low-level input hooks ──┼── kvm events ──────────►│  /dev/uinput injection   │
 │                          │   UDP 24800 (AES-GCM)   │                          │
 │  discovery answers  ◄────┼── code-sealed query ────┤  (broadcast)             │
 │  mouse moves ────────────┼── latest position ─────►│                          │
 └──────────────────────────┘                         └──────────────────────────┘
 Both: local UI at http://127.0.0.1:24801 shown in an Edge/Chrome app window.
```

One Go program per OS (stdlib plus `x/crypto/scrypt`, no cgo). Windows APIs are called through `syscall`; Linux uses `/dev/uinput` and the clipboard tools.

## Code map (`cmd/pcsc`)

| File | Role |
|---|---|
| `protocol.go` | TCP framing: `[u32 len][nonce][AES-256-GCM(type + payload)]`, key = scrypt(pairing code) |
| `app.go` | connection lifecycle: `serve` (PC), `dial` (laptop, auto-reconnect, finds the PC by code), message dispatch |
| `discover.go` | pairing by code: sealed UDP broadcast query, answered only by the PC with the same code |
| `fastpath.go` | UDP fast path for mouse moves (sequence-numbered, latest wins), probes for the latency readout |
| `kvm.go`, `kvm_windows.go`, `kvm_linux.go` | edge-crossing mouse/keyboard: Windows low-level hooks → events → uinput |
| `layout.go` | screen layout: the laptop as a rectangle in PC desktop coordinates, snapping, where crossings happen |
| `clipboard*.go` | Windows: user32 and `GetClipboardSequenceNumber`; Linux: wl-clipboard / xclip |
| `files.go` | Outbox watcher → chunked send → Sent; receiver writes to Inbox (never overwrites) |
| `display_windows.go` | monitor list (and GDI capture, used by the dormant screen-streaming code in `screen.go` / `viewer.html`) |
| `ui.go`, `ui.html` | local UI server and the single-page app window |
| `proc_*.go` | app window launcher, start-at-login (HKCU Run / XDG autostart) |

## Design notes

- **Pairing:** the 10-character code (about 50 bits) is the only secret. Both sides derive the AES key from it; a wrong code fails authentication on the first frame. Discovery queries are sealed with the same key, so only the matching PC can read one or answer it.
- **Mouse latency:** on Wi-Fi a single lost TCP packet stalls everything behind it for a retransmit (200 ms or more). Moves are absolute positions, so they go over UDP where a lost one is simply superseded. Buttons and keys stay on TCP (ordered, reliable) and carry the position and sequence number, so a click always lands where the cursor is. The PC sends at most about 250 moves a second. If UDP is blocked, moves fall back to TCP.
- **Screen layout:** the laptop screen is a rectangle in the PC's desktop coordinates (its own pixel size), placed against any edge of any PC monitor. The mouse enters the laptop when the pixel beyond a PC monitor edge is inside that rectangle and on no other monitor, and returns when it leaves the rectangle onto a PC monitor. Drops snap to the nearest monitor edge (at least 120 px touching, never overlapping a monitor). Saved in `layout.json`; the geometry refreshes every 2 s in case monitors change.
- **Edge crossing:** the PC parks its cursor in the middle of the primary screen and turns each hook event into a delta. The PC cursor is hidden while you're on the laptop: the system cursors are swapped for a blank one and restored from saved copies. Crossing straight back within 150 ms is ignored (stops edge bounce), and there is no crossing while a button is held.

## Known limits / roadmap

- No extended screen: it needed a permanently attached virtual monitor, which cluttered Windows' display settings, so it was removed. A future version could add a virtual monitor only while it's in use.
- Clipboard is text only; clipboards over 4 MB are dropped.
- No forward secrecy or replay protection on the TCP channel yet.
- The laptop app window must be opened once after start, so the PC learns the laptop's screen size.
- No code signing: Windows SmartScreen warns on first run.
