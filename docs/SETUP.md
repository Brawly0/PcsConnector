# Setup guide

Steps marked **(untested)** are documented from the tools' standard usage and have not yet been verified on this hardware.

## 0. Network
Put both machines on the same LAN (wired or 5 GHz Wi-Fi). Find the Windows IP with `ipconfig` and the Ubuntu IP with `ip a`. Allow inbound TCP 24800 (pcsc) on the Windows firewall when prompted.

## 1. Clipboard + file transfer (works today)
1. Run `./build.ps1` on Windows (needs Go). Copy `dist/pcsc-linux-amd64` to the laptop and `chmod +x` it.
2. Ubuntu: `sudo apt install xclip` (X11) or `wl-clipboard` (Wayland). Check with `pcsc doctor`.
3. Windows: `pcsc token` → copy secret. Then `pcsc host`.
4. Ubuntu: `pcsc join <WINDOWS_IP> --token <SECRET>`.

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
