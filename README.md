# PcsConnector

**Use an Ubuntu laptop as an extra screen for your Windows PC.** Your PC's mouse and keyboard move straight onto the laptop, the clipboard is shared, and files go across with a drag and drop.

<p align="center"><img src="docs/images/app-windows.png" alt="The PcsConnector window on Windows, connected to an Ubuntu laptop: mouse and keyboard sharing, extended screen, clipboard and files" width="420"></p>

- **Mouse & keyboard sharing:** push the mouse off the edge of your PC screen and it carries on onto the laptop, like Input Leap or Synergy. Scroll Lock switches back and forth.
- **Extended screen:** turn the laptop into a second Windows monitor (streamed; good for documents, chat and reference).
- **Clipboard:** copy on one computer, paste on the other.
- **Files:** drop files in the window to send them.

It is one small program per computer (about 7 MB, about 10 MB of RAM), with nothing else to install on Windows. Pairing takes a 10-character code, and everything between the computers is encrypted.

## Get started

**1. On the Windows PC:** download **`PcsConnector.exe`** from the [latest release](https://github.com/Brawly0/PcsConnector/releases/latest) and double-click it. It shows a pairing code.
- If Windows says *"Windows protected your PC"*, click **More info → Run anyway** (the app isn't code-signed yet).
- If Windows asks about the firewall, click **Allow**.

**2. On the Ubuntu laptop:** open a terminal and run:

```bash
curl -fsSL https://raw.githubusercontent.com/Brawly0/PcsConnector/main/install/install.sh | bash
```

Type your password when it asks. PcsConnector then opens on the laptop.

**3. Type the code from the PC into the laptop and press Connect.** That's it: the laptop finds the PC on your network by itself. From then on both reconnect automatically, even after a restart.

> **Different networks?** The laptop finds the PC automatically when both are on the same network (same Wi-Fi or router). Over a VPN such as Tailscale, open *"PC on a different network?"* on the laptop and type one of the addresses the PC lists.

## Using it

| | |
|---|---|
| **Mouse & keyboard** | In the PC window, choose whether the laptop is to the **left** or **right**, then move the mouse off that edge. Press **Scroll Lock** to switch at any time. |
| **Extended screen** | Press **Set up virtual monitor** once (Windows asks permission), then **Start extended screen**. The laptop shows it fullscreen. |
| **Clipboard** | On by default. Copy text on one computer, paste on the other. |
| **Files** | Drop files into the window. They arrive in the other computer's `PcsConnector/Inbox` folder. |

**Tips**
- Mouse lag depends mostly on the network. The PC window shows the round-trip time (it should be under about 20 ms). Wi-Fi on a crowded 2.4 GHz channel causes spikes; Ethernet or 5 GHz Wi-Fi fixes that.
- Both apps start at login so they can reconnect after a restart. Turn this off with **Start at login** at the bottom of the window.
- **Unpair** (bottom of the window) forgets the pairing. The PC then shows a new code.

## Troubleshooting

| Problem | Fix |
|---|---|
| Laptop says *"Looking for your PC…"* forever | Check that PcsConnector is open on the PC with the same code, and that both computers are on the same network. If Windows showed a firewall prompt, it must be allowed. Over a VPN, enter the PC's address instead. |
| Mouse doesn't cross to the laptop | Keep the PcsConnector window on the laptop open once after it starts (it tells the PC the laptop's screen size). Check the side (left/right) in the PC window. On the laptop, `pcsc doctor` should say `/dev/uinput writable`. If it doesn't, run the installer again. |
| The PC window says *"slow path"* | The laptop's firewall blocks UDP 24800. Run `sudo ufw allow 24800/udp` on the laptop (the installer does this when ufw is on). |
| Clipboard doesn't sync on the laptop | Run `pcsc doctor`. It needs `wl-clipboard` (Wayland) or `xclip` (X11); the installer installs the right one. |

Logs are in the window under **Activity log**. On the laptop, also check `/tmp/pcsc.log`.

## Requirements

- **PC:** Windows 10 or 11, 64-bit. Edge or Chrome for the app window (Edge comes with Windows).
- **Laptop:** Ubuntu 22.04 or newer, or another systemd-based Linux desktop. Works on Wayland and X11; amd64 or arm64.
- Both computers on the same network, or connected over a VPN like Tailscale.
- Ports: TCP and UDP **24800**.

## Security

The pairing code is the shared secret. A key is derived from it (scrypt), and every message is encrypted and authenticated with AES-256-GCM. A computer without the code can't connect, read anything, or even see the PC answer when the laptop searches the network for it. There is no forward secrecy or replay protection yet, so use it on your own network or a VPN, and never expose port 24800 to the internet. The window's local control page only listens on `127.0.0.1`.

## Build from source

You need [Go](https://go.dev/dl/) 1.27 or newer. On Windows run `./build.ps1`, which builds everything into `dist/`: `PcsConnector.exe`, `pcsc.exe` (the same app with a console), `pcsc-linux-amd64`, `pcsc-linux-arm64`, `install.sh`, and an offline Ubuntu bundle. `go test ./...` runs the tests. There is also a command line: `pcsc host`, `pcsc join <address> --token CODE`, `pcsc doctor`, `pcsc version`.

How it works: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) · Manual setup and advanced notes: [docs/SETUP.md](docs/SETUP.md)

## Credits

The extended screen uses the [Virtual Display Driver](https://github.com/VirtualDrivers/Virtual-Display-Driver) by MikeTheTech and contributors (MIT), bundled and installed with one click.

## License

MIT, see [LICENSE](LICENSE).
