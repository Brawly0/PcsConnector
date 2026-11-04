# Setup guide

Steps marked **(untested)** are documented from the tools' standard usage and have not yet been verified on this hardware.

## 0. Network
Put both machines on the same LAN (wired or 5 GHz Wi-Fi). Find the Windows IP with `ipconfig` and the Ubuntu IP with `ip a`. Allow inbound TCP 24800 (pcsc) on the Windows firewall when prompted.

## 1. Install and connect (app window)
**Windows PC**
1. `./build.ps1` (needs Go) → `dist/PcsConnector.exe` and `dist/pcsconnector-ubuntu.tar.gz`.
2. Double-click `PcsConnector.exe`; a browser window opens at `http://127.0.0.1:24801`.
3. Click **This is the Windows PC**. Note the **pairing code** and the **address** (the 192.168.x.x one).
4. Allow the Windows Firewall prompt (private networks). If it never appeared, run once as admin:
   `netsh advfirewall firewall add rule name="PcsConnector" dir=in action=allow protocol=TCP localport=24800`

**Ubuntu laptop**
1. Copy `pcsconnector-ubuntu.tar.gz` over (USB stick, `scp user@laptop:` etc.).
2. In a terminal: `tar xzf pcsconnector-ubuntu.tar.gz && bash pcsconnector-ubuntu/install.sh` (installs xclip/wl-clipboard, adds a menu entry and login autostart).
3. Open **PcsConnector** from the app menu (or run `~/.local/bin/pcsc`), click **This is the laptop**, enter the Windows address and the pairing code, press **Connect**.
4. Both headers turn green: "Connected". It reconnects by itself after reboots.

Clipboard sync is on by default (toggle in the window). Drag files into the window to send; they land in `~/PcsConnector/Inbox` on the other machine.

**Extended screen (untested):** on the Windows app press **Set up virtual monitor** (one Windows permission prompt; installs a bundled, signed virtual display driver), then **Start extended screen**. The laptop opens it fullscreen. No ffmpeg or other downloads.

## 1b. CLI alternative
`pcsc token` on Windows, `pcsc host`; on Ubuntu `pcsc join <WINDOWS_IP> --token <SECRET>`. `pcsc doctor` checks tools.

## 2. Mouse + keyboard sharing (untested)
Use a software KVM until the native version exists:
- **Deskflow** (github.com/deskflow/deskflow, actively maintained; Input Leap is archived). Windows = server, Ubuntu = client. Place the Ubuntu screen to the right/left of the Windows screen in the layout grid.
- X11 is much better supported on Ubuntu than Wayland. On Ubuntu login screen choose "Ubuntu on Xorg" if the client can't inject input.
- Turn off their clipboard sharing if you use `pcsc` for it, to avoid two tools fighting.

## 3. Extended screen (untested)
The app does this itself. **Set up virtual monitor** installs the bundled Virtual-Display-Driver (IddCx, signed, ~400 KB) via Windows SetupAPI; the app finds the monitor automatically. **Start extended screen** captures it (GDI) and streams JPEG frames over the encrypted channel to the laptop, which shows them fullscreen in an app window (double-click toggles fullscreen). In *Settings > Display* you can arrange the new monitor beside your main one.

Manual fallback if the one-click install fails: install Virtual-Display-Driver yourself, then press Start. Send the Activity log if it still fails.

Expect roughly 100-200 ms latency; fine for documents, chat and reference, not games. Clicks on the laptop are not forwarded to Windows yet (see Phase 3).

## Troubleshooting
- `join` never connects → firewall, wrong IP, or different token (wrong token silently drops the connection).
- Clipboard doesn't sync on Ubuntu → run `doctor`; on Wayland install `wl-clipboard`.
- Files don't send → the file must stop growing for ~1 s before it is picked up.
