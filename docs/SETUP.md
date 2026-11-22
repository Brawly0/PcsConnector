# Setup guide (manual and advanced)

The [README](../README.md) covers the normal setup: download on Windows, one command on Ubuntu, type the code. This page covers everything else.

## Install without the one-liner

**Offline bundle:** download `pcsconnector-ubuntu.tar.gz` from the [releases](https://github.com/Brawly0/PcsConnector/releases), copy it to the laptop, then run:

```bash
tar xzf pcsconnector-ubuntu.tar.gz && bash pcsconnector-ubuntu/install.sh
```

**Pin a version:** `curl -fsSL https://raw.githubusercontent.com/Brawly0/PcsConnector/main/install/install.sh | PCSC_VERSION=v1.0.0 bash`

**What the installer does**, all of it undone by deleting the listed files:
- `/etc/udev/rules.d/60-pcsconnector-uinput.rules` and `/etc/modules-load.d/pcsconnector.conf` give your login session access to `/dev/uinput`, which is how the PC's mouse and keyboard are typed into the laptop. It also grants the current session access right away, so you don't need to log out.
- If ufw is active, it adds `ufw allow 24800/udp` (remove it with `sudo ufw delete allow 24800/udp`).
- If missing, it installs `wl-clipboard` (Wayland) or `xclip` (X11), plus Chromium when no Chromium-family browser exists (the app window uses one).
- It installs `~/.local/bin/pcsc`, a menu entry in `~/.local/share/applications/`, an icon, and a login autostart entry in `~/.config/autostart/`.

**Uninstall (laptop):** `pkill -x pcsc; rm -f ~/.local/bin/pcsc ~/.local/share/applications/pcsconnector.desktop ~/.config/autostart/pcsconnector.desktop; sudo rm -f /etc/udev/rules.d/60-pcsconnector-uinput.rules /etc/modules-load.d/pcsconnector.conf`. Your files and pairing live in `~/PcsConnector`.

**Uninstall (Windows):** turn off **Start at login** in the window (or delete `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\PcsConnector`), close the app, and delete the exe. Data lives in `%USERPROFILE%\PcsConnector`.

## Networking

- One port: **24800**, TCP (the encrypted connection, made from the laptop to the PC) and UDP (finding the PC by code, and the low-latency mouse path).
- **Same network:** the laptop finds the PC by broadcasting a query that only a PC showing the same code can decrypt. Nothing to configure.
- **VPN (Tailscale, ZeroTier, WireGuard):** broadcasts don't cross a VPN, so enter the PC's VPN address on the laptop (under *"PC on a different network?"*). The PC lists its addresses under *"Laptop on a different network?"*.
- **Windows firewall:** allow PcsConnector when Windows asks. If you dismissed the prompt, allow it in *Windows Security → Firewall → Allow an app*, or run this as admin:
  `netsh advfirewall firewall add rule name="PcsConnector" dir=in action=allow program="C:\path\to\PcsConnector.exe"`
- **Lag:** the PC window shows the round trip to the laptop. Under about 20 ms feels local. Big spikes almost always come from Wi-Fi: congested 2.4 GHz channels, a weak signal, or Wi-Fi power saving (`iw dev <wlan> get power_save` on the laptop). Ethernet or 5 GHz is the real fix.

## Command line

`pcsc.exe` (Windows, from the release) and `pcsc` (Linux) are the same program as the app:

```
pcsc                        open the app window
pcsc host [--port N]        run as the PC without a window
pcsc join [ip] [--port N]   run as the laptop without a window (no ip: find the PC by code)
pcsc doctor                 check clipboard and sound tools, /dev/uinput access, browser
pcsc version
options: --token CODE  --no-clipboard      env: PCSC_TOKEN, PCSC_HOME (data folder)
```

## Data folders

Both computers keep `config.json` (pairing), `Inbox/` (received files), `Outbox/` (anything dropped here is sent) and `Sent/` in `~/PcsConnector` (Windows: `%USERPROFILE%\PcsConnector`). Set `PCSC_HOME` to use another folder.
