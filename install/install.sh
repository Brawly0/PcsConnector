#!/usr/bin/env bash
# PcsConnector installer for Ubuntu. Run:  bash install.sh
set -e
here="$(cd "$(dirname "$0")" && pwd)"
bin="$here/pcsc"
[ -f "$bin" ] || bin="$here/pcsc-linux-amd64"
[ -f "$bin" ] || { echo "pcsc binary not found next to install.sh"; exit 1; }

echo "==> Installing helper tools (asks for your password once)"
if [ -n "$WAYLAND_DISPLAY" ]; then clip="wl-clipboard"; else clip="xclip"; fi
sudo apt-get update -qq
sudo apt-get install -y "$clip" ffmpeg
# the app window uses a Chromium-family browser in app mode; install one if none is present
have=; for b in google-chrome chromium chromium-browser microsoft-edge; do command -v $b >/dev/null 2>&1 && have=1; done
[ -n "$have" ] || sudo snap install chromium || true

echo "==> Installing PcsConnector"
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/applications" "$HOME/.config/autostart"
install -m 755 "$bin" "$HOME/.local/bin/pcsc"

cat > "$HOME/.local/share/applications/pcsconnector.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=PcsConnector
Comment=Use this laptop as an extra screen for your Windows PC
Exec=$HOME/.local/bin/pcsc
Terminal=false
Categories=Network;Utility;
EOF
# start automatically at login (reconnects by itself once paired)
cp "$HOME/.local/share/applications/pcsconnector.desktop" "$HOME/.config/autostart/pcsconnector.desktop"
echo "X-GNOME-Autostart-enabled=true" >> "$HOME/.config/autostart/pcsconnector.desktop"

echo
echo "Done. Open 'PcsConnector' from your app menu (or run: ~/.local/bin/pcsc)."
echo "Then choose 'This is the laptop' and enter the address + code shown on the Windows PC."
if [ -n "$WAYLAND_DISPLAY" ]; then
  echo "Note: you are on Wayland. Clipboard works; for mouse/keyboard sharing log in with 'Ubuntu on Xorg'."
fi
