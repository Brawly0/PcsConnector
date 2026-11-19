#!/usr/bin/env bash
# PcsConnector installer for Ubuntu (and other systemd Linux desktops).
#
#   curl -fsSL https://raw.githubusercontent.com/Brawly0/PcsConnector/main/install/install.sh | bash
#
# or, from an unpacked release bundle:  bash install.sh
# Safe to re-run: it updates the app, keeps your pairing, and restarts it.
repo="Brawly0/PcsConnector"
ver="${PCSC_VERSION:-latest}" # e.g. PCSC_VERSION=v1.0.0 to pin a release

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "Unsupported CPU: $(uname -m) (PcsConnector ships amd64 and arm64)"; exit 1 ;;
esac

# 0) the binary: next to this script (release bundle) or downloaded from GitHub Releases
here="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)"
bin=""
for f in "$here/pcsc" "$here/pcsc-linux-$arch"; do [ -f "$f" ] && bin="$f" && break; done
if [ -z "$bin" ]; then
  if [ "$ver" = latest ]; then url="https://github.com/$repo/releases/latest/download/pcsc-linux-$arch"
  else url="https://github.com/$repo/releases/download/$ver/pcsc-linux-$arch"; fi
  echo "==> Downloading PcsConnector ($arch)"
  bin="$(mktemp)"
  if ! curl -fL --progress-bar -o "$bin" "$url"; then
    echo "Download failed: $url"; echo "Check your internet connection, or get it from https://github.com/$repo/releases"; exit 1
  fi
fi

echo "==> Asking for your password once (sudo)"
sudo -v || { echo "sudo failed; nothing changed. Run the installer again."; exit 1; }

# 1) keyboard/mouse sharing first, so nothing later can skip it
echo "==> Allowing mouse/keyboard sharing (/dev/uinput)"
echo 'KERNEL=="uinput", SUBSYSTEM=="misc", TAG+="uaccess", OPTIONS+="static_node=uinput"' | sudo tee /etc/udev/rules.d/60-pcsconnector-uinput.rules >/dev/null
echo uinput | sudo tee /etc/modules-load.d/pcsconnector.conf >/dev/null
sudo modprobe uinput || true
sudo udevadm control --reload-rules || true
sudo udevadm trigger --action=change /dev/uinput 2>/dev/null || sudo udevadm trigger || true
sleep 1
if [ ! -w /dev/uinput ]; then
  # the rule covers future logins; grant this session access now so no logout is needed
  sudo setfacl -m "u:$USER:rw" /dev/uinput 2>/dev/null || sudo chmod 0660 /dev/uinput
fi

# 2) firewall: if ufw is on, let the PC reach this laptop on UDP 24800 (finding the PC by code, and
#    the low-latency mouse path). Everything on it is encrypted with the pairing code.
if command -v ufw >/dev/null 2>&1 && sudo ufw status 2>/dev/null | grep -q "Status: active"; then
  echo "==> Opening UDP 24800 in the firewall (ufw)"
  sudo ufw allow 24800/udp comment PcsConnector >/dev/null || true
fi

# 3) clipboard tool for the desktop session type (decided by the session, not by this shell, so it is
#    right even when installing over ssh)
uid="$(id -u)"
if [ -n "$WAYLAND_DISPLAY" ] || ls /run/user/"$uid"/wayland-* >/dev/null 2>&1; then clip=wl-clipboard; tool=wl-copy
else clip=xclip; tool=xclip; fi
if ! command -v "$tool" >/dev/null 2>&1; then
  echo "==> Installing $clip (clipboard sharing)"
  sudo apt-get install -y "$clip" >/dev/null 2>&1 || { sudo apt-get update -qq && sudo apt-get install -y "$clip"; } || echo "WARNING: could not install $clip; clipboard sharing needs it"
fi
# the app window uses a Chromium-family browser in app mode
have=; for b in google-chrome chromium chromium-browser microsoft-edge brave-browser; do command -v $b >/dev/null 2>&1 && have=1; done
if [ -z "$have" ]; then echo "==> Installing Chromium (for the app window)"; sudo snap install chromium || true; fi

# 4) the app: stop the old one AND its window (an old window keeps running the previous version's page)
echo "==> Installing PcsConnector"
pkill -x pcsc 2>/dev/null
pkill -f -- '--app=http://127.0.0.1:24801' 2>/dev/null
sleep 1
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/applications" "$HOME/.config/autostart"
install -m 755 "$bin" "$HOME/.local/bin/pcsc" || { echo "Could not install the app to ~/.local/bin"; exit 1; }
icon="$HOME/.local/share/icons/hicolor/scalable/apps/pcsconnector.svg"
mkdir -p "$(dirname "$icon")"
cat > "$icon" <<'EOF'
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="9" fill="#4f46e5"/><rect x="5" y="9" width="12" height="9" rx="1.5" fill="none" stroke="#fff" stroke-width="2"/><rect x="18" y="12" width="9" height="7" rx="1.5" fill="none" stroke="#fff" stroke-width="2"/><path d="M8 22h6M20 22h5" stroke="#fff" stroke-width="2" stroke-linecap="round"/></svg>
EOF
cat > "$HOME/.local/share/applications/pcsconnector.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=PcsConnector
Comment=Use this laptop as an extra screen for your Windows PC
Exec=$HOME/.local/bin/pcsc
Icon=pcsconnector
Terminal=false
Categories=Network;Utility;
EOF
# start at login (reconnects by itself once paired); can be turned off in the app window
[ -f "$HOME/.config/autostart/pcsconnector.desktop" ] || {
  cp "$HOME/.local/share/applications/pcsconnector.desktop" "$HOME/.config/autostart/pcsconnector.desktop"
  echo "X-GNOME-Autostart-enabled=true" >> "$HOME/.config/autostart/pcsconnector.desktop"
}

echo
"$HOME/.local/bin/pcsc" doctor
echo

# 5) start it inside the desktop session, even when this ran over ssh
if systemd-run --user --collect --quiet --unit="pcsconnector-$(date +%s)" \
     /bin/sh -c 'exec "$HOME/.local/bin/pcsc" >/tmp/pcsc.log 2>&1' 2>/dev/null; then
  started=1
elif [ -n "$WAYLAND_DISPLAY$DISPLAY" ]; then
  nohup "$HOME/.local/bin/pcsc" >/tmp/pcsc.log 2>&1 &
  started=1
fi
if [ -n "$started" ]; then
  echo "Done. PcsConnector is opening on the laptop's screen."
else
  echo "Done. Open PcsConnector from the app menu on the laptop."
fi
echo "Type the code shown on your Windows PC and press Connect."
