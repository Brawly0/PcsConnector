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
2. In a terminal: `tar xzf pcsconnector-ubuntu.tar.gz && bash pcsconnector-ubuntu/install.sh` (installs xclip/wl-clipboard + ffmpeg, adds a menu entry and login autostart).
3. Open **PcsConnector** from the app menu (or run `~/.local/bin/pcsc`), click **This is the laptop**, enter the Windows address and the pairing code, press **Connect**.
4. Both headers turn green: "Connected". It reconnects by itself after reboots.

Clipboard sync is on by default (toggle in the window). Drag files into the window to send; they land in `~/PcsConnector/Inbox` on the other machine.

**Extended screen (untested):** install a virtual display on Windows (section 3), install `ffmpeg` on Windows (`winget install Gyan.FFmpeg`), enter the virtual monitor's position/size in the app, press **Start extended screen**. The laptop opens it fullscreen; stop it from the Windows app.

## 1b. CLI alternative
`pcsc token` on Windows, `pcsc host`; on Ubuntu `pcsc join <WINDOWS_IP> --token <SECRET>`. `pcsc doctor` checks tools.

## 2. Mouse + keyboard sharing (untested)
Use a software KVM until the native version exists:
- **Deskflow** (github.com/deskflow/deskflow, actively maintained; Input Leap is archived). Windows = server, Ubuntu = client. Place the Ubuntu screen to the right/left of the Windows screen in the layout grid.
- X11 is much better supported on Ubuntu than Wayland. On Ubuntu login screen choose "Ubuntu on Xorg" if the client can't inject input.
- Turn off their clipboard sharing if you use `pcsc` for it, to avoid two tools fighting.

## 3. Extended screen (untested)
Windows needs a second (virtual) monitor to extend onto, since the laptop is not physically attached.
1. Install a virtual display driver, e.g. **Virtual-Display-Driver** (IddCx, github.com/itsmikethetech/Virtual-Display-Driver). In *Settings → Display*, set it to *Extend*, 1920x1080, and arrange it beside your main monitor. Note its top-left offset (e.g. main is 1920 wide → virtual starts at x=1920).
2. Windows – capture that region and stream it:
   ```
   ffmpeg -f gdigrab -framerate 30 -offset_x 1920 -offset_y 0 -video_size 1920x1080 -i desktop ^
     -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p ^
     -f mpegts "udp://<UBUNTU_IP>:5000?pkt_size=1316"
   ```
   (Use `h264_nvenc` / `h264_qsv` instead of `libx264` if you have a GPU encoder – lower CPU, lower latency.)
3. Ubuntu – play fullscreen:
   ```
   ffplay -fflags nobuffer -flags low_delay -framedrop -probesize 32 -analyzeduration 0 -fs udp://0.0.0.0:5000
   ```
4. With step 2 running, move the mouse off the edge of the Windows screen onto the virtual monitor — it shows up on the laptop, and the KVM tool from section 2 handles input only for the *laptop's own* screen. To click windows shown in the stream, you move the Windows mouse onto the virtual monitor region (requires the Phase 3 input forwarding below to feel seamless).

Expect ~100–200 ms latency with this approach; good for documents/chat/reference, not games.

## Troubleshooting
- `join` never connects → firewall, wrong IP, or different token (wrong token silently drops the connection).
- Clipboard doesn't sync on Ubuntu → run `doctor`; on Wayland install `wl-clipboard`.
- Files don't send → the file must stop growing for ~1 s before it is picked up.
