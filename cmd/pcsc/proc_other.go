//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func hideWindow(c *exec.Cmd) {}

func autostartFile() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "autostart", "pcsconnector.desktop")
}

// autostartOn reports whether PcsConnector starts when you log in (XDG autostart entry).
func autostartOn() bool {
	_, err := os.Stat(autostartFile())
	return err == nil
}

// setAutostart writes or removes the XDG autostart entry, which reconnects by itself after a reboot.
func setAutostart(on bool) error {
	f := autostartFile()
	if !on {
		os.Remove(f)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(f), 0o755)
	return os.WriteFile(f, []byte(fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=PcsConnector
Comment=Use this laptop as an extra screen for your Windows PC
Exec=%s
Terminal=false
X-GNOME-Autostart-enabled=true
`, exe)), 0o644)
}

// openApp shows url in a standalone app window (Chromium-family --app), falling back to the default browser.
func openApp(url string) {
	for _, b := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
		if p, err := exec.LookPath(b); err == nil {
			if exec.Command(p, "--app="+url, "--window-size=520,760").Start() == nil {
				return
			}
		}
	}
	exec.Command("xdg-open", url).Start()
}

// openAppFull shows url in a fullscreen app window.
func openAppFull(url string) {
	for _, b := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
		if p, err := exec.LookPath(b); err == nil {
			if exec.Command(p, "--app="+url, "--start-fullscreen").Start() == nil {
				return
			}
		}
	}
	exec.Command("xdg-open", url).Start()
}
