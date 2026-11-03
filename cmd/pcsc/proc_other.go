//go:build !windows

package main

import "os/exec"

func hideWindow(c *exec.Cmd) {}

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
